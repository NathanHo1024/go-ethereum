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

package accounts

import (
	"reflect"
	"sort"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/event"
)

// Config contains the settings of the global account manager.
// Config 包含了全局账户管理设置
// TODO(rjl493456442, karalabe, holiman): Get rid of this when account management
// is removed in favor of Clef.
// 取消帐户管理以改用Clef时，请摆脱此情况。
type Config struct {
	// cmd启动指令 -- allow-insecure-unlock
	InsecureUnlockAllowed bool // Whether account unlocking in insecure environment is allowed //是否允许在不安全的环境中解锁帐户
}

// Manager is an overarching account manager that can communicate with various
// backends for signing transactions.
// Manager 是一个账户管理用于与各种后端进行短信以及签名交易
type Manager struct {
	config   *Config                    // Global account manager configurations 全局账户管理配置 --是否允许解锁账户
	backends map[reflect.Type][]Backend // Index of backends currently registered 当前注册的后端索引 -- 后端管理
	updaters []event.Subscription       // Wallet update subscriptions for all backends 更新所有后端的钱包订阅
	updates  chan WalletEvent           // Subscription sink for backend wallet changes 后端钱包更改的订阅接收器
	wallets  []Wallet                   // Cache of all wallets from all registered backends 缓存来自所有注册后端的所有钱包

	feed event.Feed // Wallet feed notifying of arrivals/departures 钱包出入金的通知事件

	quit chan chan error
	lock sync.RWMutex
}

// 创建Manager
// NewManager creates a generic account manager to sign transaction via various
// supported backends.
// NewManager 生成一个通用的用户管理后端用来签署交易-- 传入参数， 指令，后端参数
func NewManager(config *Config, backends ...Backend) *Manager {
	// Retrieve the initial list of wallets from the backends and sort by URL
	// 从后端检索钱包的初始列表，然后按URL排序
	var wallets []Wallet //初始化一个wallet数组
	for _, backend := range backends {
		// 进行backend的钱包内容移植
		wallets = merge(wallets, backend.Wallets()...)
	}
	// Subscribe to wallet notifications from all backends
	// 订阅所有后端的钱包通知 --  创建一个WalletEvent类型（backend订阅槽），长度为后端4倍的数组
	updates := make(chan WalletEvent, 4*len(backends))

	subs := make([]event.Subscription, len(backends)) //创建订阅数组， 长度跟后端一样
	for i, backend := range backends {
		//传入后端数组里面的每一个参数都进行updates新事件的订阅
		subs[i] = backend.Subscribe(updates)
	}
	// Assemble the account manager and return
	// 实例化账户管理 并且返回
	am := &Manager{
		config:   config,                           //指令
		backends: make(map[reflect.Type][]Backend), //后端参数
		updaters: subs,                             //更新订阅事件
		updates:  updates,                          //后端钱包更改的订阅接收器
		wallets:  wallets,                          //钱包
		quit:     make(chan chan error),            //退出的channel
	}
	for _, backend := range backends {
		//遍历每一个后端，将每一个对应的key与后端进行绑定
		kind := reflect.TypeOf(backend)
		am.backends[kind] = append(am.backends[kind], backend)
	}
	go am.update() //开启线程更新

	return am //返回manager
}

// Close terminates the account manager's internal notification processes.
// Close 终止用户的内部管理通知进程
func (am *Manager) Close() error {
	errc := make(chan error)
	am.quit <- errc
	return <-errc
}

// Config returns the configuration of account manager.
// Config 返回当前account manager的指令
func (am *Manager) Config() *Config {
	return am.config
}

// update is the wallet event loop listening for notifications from the backends
// and updating the cache of wallets.
// update 是钱包事件循环用于监听来自后端的通知并且更新钱包缓存的
func (am *Manager) update() {
	// Close all subscriptions when the manager terminates
	// 当manager终止后，关闭所有的订阅
	defer func() {
		am.lock.Lock() //加锁
		for _, sub := range am.updaters {
			sub.Unsubscribe() //遍历每一个订阅，进行解绑
		}
		am.updaters = nil //将订阅置为nil
		am.lock.Unlock()  //解锁
	}()

	// Loop until termination
	// 循环直到终止
	for {
		select {
		case event := <-am.updates: // 从订阅的channel获取值
			// Wallet event arrived, update local cache 钱包事件到达，更新本地的缓存
			am.lock.Lock()      //加锁
			switch event.Kind { //事件类型
			case WalletArrived: //如果是钱包进来， 做排序操作，更新钱包
				am.wallets = merge(am.wallets, event.Wallet)
			case WalletDropped: //如果是钱包出去， 做删除钱包的操作
				am.wallets = drop(am.wallets, event.Wallet)
			}
			am.lock.Unlock() //解锁

			// Notify any listeners of the event
			// 通知事件的监听器
			am.feed.Send(event) //推送事件给后端

		case errc := <-am.quit: //从退出的channel获取值
			// Manager terminating, return
			// 当manager终止， 返回推送nil
			errc <- nil
			return
		}
	}
}

// Backends retrieves the backend(s) with the given type from the account manager.
// Backends 检索指定Type类型的后端并且返回
func (am *Manager) Backends(kind reflect.Type) []Backend {
	return am.backends[kind]
}

// Wallets returns all signer accounts registered under this account manager.
// Wallets 返回Manager缓存的所有钱包内容
func (am *Manager) Wallets() []Wallet {
	am.lock.RLock()         //只读锁
	defer am.lock.RUnlock() //解锁

	return am.walletsNoLock() ////拿到的是am.wallets的复制内容
}

// walletsNoLock returns all registered wallets. Callers must hold am.lock.
// walletsNoLock 返回所有已注册的钱包。调用者必须携带am.lock
func (am *Manager) walletsNoLock() []Wallet {
	cpy := make([]Wallet, len(am.wallets))
	copy(cpy, am.wallets)
	return cpy
}

// Wallet retrieves the wallet associated with a particular URL.
// Wallet 检索与特定URL关联的钱包。
func (am *Manager) Wallet(url string) (Wallet, error) {
	am.lock.RLock()         //只读锁
	defer am.lock.RUnlock() //延迟执行解锁

	parsed, err := parseURL(url) //解析url
	if err != nil {              //如果err不为nil，返回异常信息
		return nil, err
	}
	for _, wallet := range am.walletsNoLock() { //开始遍历钱包
		if wallet.URL() == parsed { //如果解析的内容 跟当前的url一样  直接返回
			return wallet, nil
		}
	}
	return nil, ErrUnknownWallet //否则返回未知钱包异常信息
}

// Accounts returns all account addresses of all wallets within the account manager
// Accounts 返回帐户管理器中所有钱包的所有帐户地址
func (am *Manager) Accounts() []common.Address {
	am.lock.RLock()         //只读锁
	defer am.lock.RUnlock() //延迟解锁

	addresses := make([]common.Address, 0) // return [] instead of nil if empty//创建一个地址数组
	for _, wallet := range am.wallets {    //遍历账户管理器的wallet内容
		for _, account := range wallet.Accounts() { //再遍历钱包的地址列表
			addresses = append(addresses, account.Address) //将地址追加到新创的数组
		}
	}
	return addresses
}

// Find attempts to locate the wallet corresponding to a specific account. Since
// accounts can be dynamically added to and removed from wallets, this method has
// a linear runtime in the number of wallets.
// Find 尝试找到对应于特定帐户的钱包。由于可以在钱包中动态添加和删除帐户，因此此方法在钱包数量方面具有线性运行时间。
func (am *Manager) Find(account Account) (Wallet, error) { //account做参
	am.lock.RLock()         //只读锁
	defer am.lock.RUnlock() //延迟解锁

	for _, wallet := range am.wallets { //遍历钱包
		if wallet.Contains(account) { //如果钱包里面包含了account 返回
			return wallet, nil
		}
	}
	return nil, ErrUnknownAccount //否则抛异常--未知地址异常
}

// Subscribe creates an async subscription to receive notifications when the
// manager detects the arrival or departure of a wallet from any of its backends.
// Subscribe 创建一个异常订阅用于接受通知 当manager检测到wallet离开或者到达后端的时候
func (am *Manager) Subscribe(sink chan<- WalletEvent) event.Subscription { //当前ma做参， 新增一个WalletEvent的channel做参
	return am.feed.Subscribe(sink) //am.feed 订阅sink频道
}

// merge is a sorted analogue of append for wallets, where the ordering of the
// origin list is preserved by inserting new wallets at the correct position.
// 钱包排序
// The original slice is assumed to be already sorted by URL.
func merge(slice []Wallet, wallets ...Wallet) []Wallet {
	for _, wallet := range wallets { //遍历钱包
		n := sort.Search(len(slice), func(i int) bool { return slice[i].URL().Cmp(wallet.URL()) >= 0 }) //钱包进行排序
		if n == len(slice) {
			slice = append(slice, wallet)
			continue
		}
		slice = append(slice[:n], append([]Wallet{wallet}, slice[n:]...)...)
	}
	return slice
}

// drop is the couterpart of merge, which looks up wallets from within the sorted
// cache and removes the ones specified.
// drop 是merge的反义词， 从分类中查找钱包缓存并删除指定的内容。
func drop(slice []Wallet, wallets ...Wallet) []Wallet {
	for _, wallet := range wallets {
		n := sort.Search(len(slice), func(i int) bool { return slice[i].URL().Cmp(wallet.URL()) >= 0 })
		if n == len(slice) {
			// Wallet not found, may happen during startup
			continue
		}
		slice = append(slice[:n], slice[n+1:]...)
	}
	return slice
}
