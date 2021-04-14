// Copyright 2017 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package usbwallet

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/karalabe/usb"
)

// LedgerScheme is the protocol scheme prefixing account and wallet URLs.
// LedgerScheme 是钱包和wallet的URL前缀协议
const LedgerScheme = "ledger"

// TrezorScheme is the protocol scheme prefixing account and wallet URLs.
// TrezorScheme 是钱包和wallet的URL前缀协议
const TrezorScheme = "trezor"

// refreshCycle is the maximum time between wallet refreshes (if USB hotplug
// notifications don't work).
// 刷新周期是钱包之前的最大刷新秒数
const refreshCycle = time.Second

// refreshThrottling is the minimum time between wallet refreshes to avoid USB
// trashing.
// refreshThrottling 是钱包之间的最小刷新时间---避免垃圾交易
const refreshThrottling = 500 * time.Millisecond

// Hub is a accounts.Backend that can find and handle generic USB hardware wallets.
// Hub 是一个账户后端用于可以找到以及解决通用的usb硬件钱包
type Hub struct {
	scheme     string                  // Protocol scheme prefixing account and wallet URLs. 账户，钱包前端的协议
	vendorID   uint16                  // USB vendor identifier used for device discovery 用于设备发现usb的供应商标识符
	productIDs []uint16                // USB product identifiers used for device discovery 用于设备发现usb的产品标识符
	usageID    uint16                  // USB usage page identifier used for macOS device discovery macOs系统发现usb的使用页面标识符
	endpointID int                     // USB endpoint identifier used for non-macOS device discovery 用于非macOS系统发现usb的端点标识符
	makeDriver func(log.Logger) driver // Factory method to construct a vendor specific driver 工厂方法来构造特定于供应商的驱动

	refreshed   time.Time               // Time instance when the list of wallets was last refreshed 钱包列表最近刷新时间的列表
	wallets     []accounts.Wallet       // List of USB wallet devices currently tracking USB钱包当前跟踪设备的列表
	updateFeed  event.Feed              // Event feed to notify wallet additions/removals 事件用于通知钱包的加入和移除
	updateScope event.SubscriptionScope // Subscription scope tracking current live listeners 订阅范围跟踪当前的实时监听器
	updating    bool                    // Whether the event notification loop is running 当事件通知循环真正运行

	//	//退出channel
	quit chan chan error

	//保护集线器的内部不受快速访问
	stateLock sync.RWMutex // Protects the internals of the hub from racey access

	// TODO(karalabe): remove if hotplug lands on Windows
	commsPend int        // Number of operations blocking enumeration 阻塞枚举的操作数
	commsLock sync.Mutex // Lock protecting the pending counter and enumeration 保护挂起计算器和枚举的锁
	enumFails uint32     // Number of times enumeration has failed 枚举失败的次数
}

// NewLedgerHub creates a new hardware wallet manager for Ledger devices.
// NewLedgerHub 创建一个新的硬件钱包用于管理分类设备
func NewLedgerHub() (*Hub, error) {
	return newHub(LedgerScheme, 0x2c97, []uint16{
		// Original product IDs
		0x0000, /* Ledger Blue */
		0x0001, /* Ledger Nano S */
		0x0004, /* Ledger Nano X */

		// Upcoming product IDs: https://www.ledger.com/2019/05/17/windows-10-update-sunsetting-u2f-tunnel-transport-for-ledger-devices/
		0x0015, /* HID + U2F + WebUSB Ledger Blue */
		0x1015, /* HID + U2F + WebUSB Ledger Nano S */
		0x4015, /* HID + U2F + WebUSB Ledger Nano X */
		0x0011, /* HID + WebUSB Ledger Blue */
		0x1011, /* HID + WebUSB Ledger Nano S */
		0x4011, /* HID + WebUSB Ledger Nano X */
	}, 0xffa0, 0, newLedgerDriver)
}

// NewTrezorHubWithHID creates a new hardware wallet manager for Trezor devices.
func NewTrezorHubWithHID() (*Hub, error) {
	return newHub(TrezorScheme, 0x534c, []uint16{0x0001 /* Trezor HID */}, 0xff00, 0, newTrezorDriver)
}

// NewTrezorHubWithWebUSB creates a new hardware wallet manager for Trezor devices with
// firmware version > 1.8.0
func NewTrezorHubWithWebUSB() (*Hub, error) {
	return newHub(TrezorScheme, 0x1209, []uint16{0x53c1 /* Trezor WebUSB */}, 0xffff /* No usage id on webusb, don't match unset (0) */, 0, newTrezorDriver)
}

// newHub creates a new hardware wallet manager for generic USB devices.
// newHub 创建一个新的硬件钱包用于管理通用的usb设备
func newHub(scheme string, vendorID uint16, productIDs []uint16, usageID uint16, endpointID int, makeDriver func(log.Logger) driver) (*Hub, error) {
	if !usb.Supported() { //判断当前系统是否支持usb
		return nil, errors.New("unsupported platform")
	}
	hub := &Hub{ //指针创建
		scheme:     scheme,   //协议
		vendorID:   vendorID, //下列是各种设备标识符
		productIDs: productIDs,
		usageID:    usageID,
		endpointID: endpointID,
		makeDriver: makeDriver,            //驱动
		quit:       make(chan chan error), //退出的channel
	}
	hub.refreshWallets() //扫描当前设备所支持的usb钱包
	return hub, nil
}

// Wallets implements accounts.Backend, returning all the currently tracked USB
// devices that appear to be hardware wallets.
func (hub *Hub) Wallets() []accounts.Wallet {
	// Make sure the list of wallets is up to date
	hub.refreshWallets()

	hub.stateLock.RLock()
	defer hub.stateLock.RUnlock()

	cpy := make([]accounts.Wallet, len(hub.wallets))
	copy(cpy, hub.wallets)
	return cpy
}

// refreshWallets scans the USB devices attached to the machine and updates the
// list of wallets based on the found devices.
// refreshWallets 扫描连接到机器的USB设备，并根据找到的设备更新钱包列表。
func (hub *Hub) refreshWallets() {
	// Don't scan the USB like crazy it the user fetches wallets in a loop
	// 不要疯狂扫描usb，用户会循环拿到钱包
	hub.stateLock.RLock()                //只读锁-- 保护集线器的内部不受快速访问
	elapsed := time.Since(hub.refreshed) //计算上次钱包刷新时间
	hub.stateLock.RUnlock()              //解锁

	if elapsed < refreshThrottling { //如果上次调用的时间小于钱包刷新的最小时间 直接返回
		return
	}
	// If USB enumeration is continually failing, don't keep trying indefinitely
	// 如果USB枚举持续失败，请不要无限期尝试
	if atomic.LoadUint32(&hub.enumFails) > 2 { //如果枚举失败的次数大于2 直接返回
		return
	}
	// Retrieve the current list of USB wallet devices
	// 检索USB钱包设备的当前列表
	var devices []usb.DeviceInfo //设备信息数组

	if runtime.GOOS == "linux" { //如果当前运行的系统是linux
		// hidapi on Linux opens the device during enumeration to retrieve some infos,
		// breaking the Ledger protocol if that is waiting for user confirmation. This
		// is a bug acknowledged at Ledger, but it won't be fixed on old devices so we
		// need to prevent concurrent comms ourselves. The more elegant solution would
		// be to ditch enumeration in favor of hotplug events, but that don't work yet
		// on Windows so if we need to hack it anyway, this is more elegant for now.
		hub.commsLock.Lock()   //保护挂起计算器和枚举的锁 上锁
		if hub.commsPend > 0 { // A confirmation is pending, don't refresh  一个确认再pending  不要刷新
			hub.commsLock.Unlock() //解锁
			return
		}
	}
	//如果操作系统不是linux 或则是linux 但是没有pending的消息--
	infos, err := usb.Enumerate(hub.vendorID, 0) //返回支持的设备信息
	if err != nil {
		failcount := atomic.AddUint32(&hub.enumFails, 1) //枚举失败次数
		if runtime.GOOS == "linux" {                     //如果操作系统是linux  解锁
			// See rationale before the enumeration why this is needed and only on Linux.
			hub.commsLock.Unlock()
		}
		log.Error("Failed to enumerate USB devices", "hub", hub.scheme,
			"vendor", hub.vendorID, "failcount", failcount, "err", err)
		return
	}
	atomic.StoreUint32(&hub.enumFails, 0) //重置枚举失败次数

	for _, info := range infos { //遍历设备信息
		for _, id := range hub.productIDs { //遍历用于设备发现usb的产品标识符
			// Windows and Macos use UsageID matching, Linux uses Interface matching
			// windows和mac 用UsageID匹配，  linux用接口
			if info.ProductID == id && (info.UsagePage == hub.usageID || info.Interface == hub.endpointID) {
				devices = append(devices, info)
				break
			}
		}
	}
	if runtime.GOOS == "linux" { //如果操作系统是linux 并且没有pending的信息
		// See rationale before the enumeration why this is needed and only on Linux.
		hub.commsLock.Unlock()
	}
	// Transform the current list of wallets into the new one
	// 将当前的钱包列表转换为新的钱包列表
	hub.stateLock.Lock() // 保护集线器的内部不受快速访问

	var ( //定义两个数组1. Wallet类型的数组，长度为设备信息的长度，    2.钱包事件的数组
		wallets = make([]accounts.Wallet, 0, len(devices))
		events  []accounts.WalletEvent
	)

	for _, device := range devices { //遍历设备
		url := accounts.URL{Scheme: hub.scheme, Path: device.Path} //当前钱包设备的协议，平台路径 生成了accounts的URL

		// Drop wallets in front of the next device or those that failed for some reason
		// 将钱包放在下一台设备的前面或由于某种原因而失败的钱包
		for len(hub.wallets) > 0 { //当前钱包数量大于0执行
			// Abort if we're past the current device and found an operational one
			_, failure := hub.wallets[0].Status()
			if hub.wallets[0].URL().Cmp(url) >= 0 || failure == nil {
				break
			}
			// Drop the stale and failed devices
			// 删除陈旧和故障的设备
			events = append(events, accounts.WalletEvent{Wallet: hub.wallets[0], Kind: accounts.WalletDropped}) //钱包被摘除
			hub.wallets = hub.wallets[1:]
		}
		// If there are no more wallets or the device is before the next, wrap new wallet
		// 如果没有更多钱包或设备在下一个之前，请包装新的钱包
		if len(hub.wallets) == 0 || hub.wallets[0].URL().Cmp(url) > 0 {
			logger := log.New("url", url)
			wallet := &wallet{hub: hub, driver: hub.makeDriver(logger), url: &url, info: device, log: logger} //创建一个wallet

			events = append(events, accounts.WalletEvent{Wallet: wallet, Kind: accounts.WalletArrived}) //对当前钱包进行一个WalletArrived事件绑定
			wallets = append(wallets, wallet)                                                           //当前钱包数组添加新的wallet（携带当前信息）
			continue
		}
		// If the device is the same as the first wallet, keep it
		// 如果设备与第一个钱包相同，请保留该设备
		if hub.wallets[0].URL().Cmp(url) == 0 {
			wallets = append(wallets, hub.wallets[0])
			hub.wallets = hub.wallets[1:]
			continue
		}
	}
	// Drop any leftover wallets and set the new batch
	// 放下所有剩余的钱包并设置新批次
	for _, wallet := range hub.wallets {
		events = append(events, accounts.WalletEvent{Wallet: wallet, Kind: accounts.WalletDropped})
	}
	hub.refreshed = time.Now()
	hub.wallets = wallets //把新的地址信息存在了当前hub的信息种
	hub.stateLock.Unlock()

	// Fire all wallet events and return
	// 更新所有新的事件
	for _, event := range events {
		hub.updateFeed.Send(event)
	}
}

// Subscribe implements accounts.Backend, creating an async subscription to
// receive notifications on the addition or removal of USB wallets.
// Subscribe 实现了 accounts.Backend, 创建一个异步订阅用于接收usb wallets的添加或者移除的通知
func (hub *Hub) Subscribe(sink chan<- accounts.WalletEvent) event.Subscription { // channel做参
	// We need the mutex to reliably start/stop the update loop
	// 我们需要互斥体来可靠地启动/停止更新循环
	hub.stateLock.Lock()         //保护集线器的内部不受快速访问 上锁
	defer hub.stateLock.Unlock() //延迟解锁

	// Subscribe the caller and track the subscriber count
	// 订阅呼叫者并跟踪订阅者数量
	sub := hub.updateScope.Track(hub.updateFeed.Subscribe(sink))

	// Subscribers require an active notification loop, start it
	// 订户需要一个活动的通知循环，然后启动它
	if !hub.updating {
		hub.updating = true
		go hub.updater()
	}
	return sub
}

// updater is responsible for maintaining an up-to-date list of wallets managed
// by the USB hub, and for firing wallet addition/removal events.
// updater 负责维护USB集线器管理的钱包的最新列表，并引发钱包添加/删除事件。
func (hub *Hub) updater() {
	for {
		// TODO: Wait for a USB hotplug event (not supported yet) or a refresh timeout
		// <-hub.changes
		time.Sleep(refreshCycle) //睡眠1秒

		// Run the wallet refresher
		// 运行钱包刷新
		hub.refreshWallets()

		// If all our subscribers left, stop the updater
		// 如果我们所有的订户都离开了，请停止更新程序
		hub.stateLock.Lock()              //保护锁上锁
		if hub.updateScope.Count() == 0 { //订阅范围跟踪当前的实时监听器如果为0 --  等于用户都不在了
			hub.updating = false   //停止更新
			hub.stateLock.Unlock() //解锁
			return
		}
		hub.stateLock.Unlock()
	}
}
