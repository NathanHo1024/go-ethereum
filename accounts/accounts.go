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

// Package accounts implements high level Ethereum account management.
package accounts

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
	"golang.org/x/crypto/sha3"
)

// Account represents an Ethereum account located at a specific location defined
// by the optional URL field.
// Account 表示一个特定位置的以太坊地址可以通过URL表示
type Account struct {
	Address common.Address `json:"address"` // Ethereum account address derived from the key
	URL     URL            `json:"url"`     // Optional resource locator within a backend
}

const (
	MimetypeDataWithValidator = "data/validator"
	MimetypeTypedData         = "data/typed"
	MimetypeClique            = "application/x-clique-header"
	MimetypeTextPlain         = "text/plain"
)

// Wallet represents a software or hardware wallet that might contain one or more
// accounts (derived from the same seed).
// 1个钱包可以包含1/多个账户
type Wallet interface {
	// URL retrieves the canonical path under which this wallet is reachable. It is
	// user by upper layers to define a sorting order over all wallets from multiple
	// backends.
	// URL 检索此钱包可访问的规范路径。上层由用户定义来自多个后端的所有钱包的排序顺序。
	URL() URL

	// Status returns a textual status to aid the user in the current state of the
	// wallet. It also returns an error indicating any failure the wallet might have
	// encountered.
	// Status 用来返回一个文本值用来标识当前钱包的状态。 同时也会返回一个error用来标识钱包遇到的任何错误。
	Status() (string, error)

	// Open initializes access to a wallet instance. It is not meant to unlock or
	// decrypt account keys, rather simply to establish a connection to hardware
	// wallets and/or to access derivation seeds.
	// Open 初始化对钱包实例的访问。这个方法并不意味着解锁或者解密账户，而是简单地建立与硬件钱包的连接和/或访问衍生种子。
	// The passphrase parameter may or may not be used by the implementation of a
	// particular wallet instance. The reason there is no passwordless open method
	// is to strive towards a uniform wallet handling, oblivious to the different
	// backend providers.
	// passphrase 参数可能在某些实现中并不需要。 没有提供一个无passphrase参数的Open方法的原因是为了提供一个统一的接口。
	// Please note, if you open a wallet, you must close it to release any allocated
	// resources (especially important when working with hardware wallets).
	// 请注意，如果你open了一个钱包，你必须close它。不然有些资源可能没有释放。 特别是使用硬件钱包的时候需要特别注意。
	Open(passphrase string) error

	// Close releases any resources held by an open wallet instance.
	// Close 释放由Open方法占用的任何资源。
	Close() error

	// Accounts retrieves the list of signing accounts the wallet is currently aware
	// of. For hierarchical deterministic wallets, the list will not be exhaustive,
	// rather only contain the accounts explicitly pinned during account derivation.
	// Accounts用来获取钱包发现了账户列表。 对于分层次的钱包， 这个列表不会详尽的列出所有的账号， 而是只包含在帐户派生期间明确固定的帐户。
	Accounts() []Account

	// Contains returns whether an account is part of this particular wallet or not.
	// Contains 返回一个地址是否属于本钱包。
	Contains(account Account) bool

	// Derive attempts to explicitly derive a hierarchical deterministic account at
	// the specified derivation path. If requested, the derived account will be added
	// to the wallet's tracked account list.
	// Derive尝试在指定的派生路径上显式派生出分层确定性帐户。 如果pin为true，派生帐户将被添加到钱包的跟踪帐户列表中。
	Derive(path DerivationPath, pin bool) (Account, error)

	// SelfDerive sets a base account derivation path from which the wallet attempts
	// to discover non zero accounts and automatically add them to list of tracked
	// accounts.
	// SelfDerive 设置一个基本帐户导出路径，从中钱包尝试发现非零帐户，并自动将其添加到跟踪帐户列表中。
	// Note, self derivation will increment the last component of the specified path
	// opposed to decending into a child path to allow discovering accounts starting
	// from non zero components.
	// 注意，SelfDerive将递增指定路径的最后一个组件，而不是下降到子路径，以允许从非零组件开始发现帐户。
	// Some hardware wallets switched derivation paths through their evolution, so
	// this method supports providing multiple bases to discover old user accounts
	// too. Only the last base will be used to derive the next empty account.
	// 一些硬件钱包通过其演变过程切换了派生路径，因此该方法还支持提供多种基础来发现旧的用户帐户。仅最后一个基数将用于派生下一个空帐户。
	// You can disable automatic account discovery by calling SelfDerive with a nil
	// chain state reader.
	// 你可以通过传递一个nil的ChainStateReader来禁用自动账号发现。
	SelfDerive(bases []DerivationPath, chain ethereum.ChainStateReader)

	// SignData requests the wallet to sign the hash of the given data
	// It looks up the account specified either solely via its address contained within,
	// or optionally with the aid of any location metadata from the embedded URL field.
	// SignData 请求钱包对数据进行hash计算。 它可以仅通过包含在其中的地址来查找指定的帐户，也可以选择使用嵌入式URL字段中的任何位置元数据来查找。
	// If the wallet requires additional authentication to sign the request (e.g.
	// a password to decrypt the account, or a PIN code o verify the transaction),
	// an AuthNeededError instance will be returned, containing infos for the user
	// about which fields or actions are needed. The user may retry by providing
	// the needed details via SignDataWithPassphrase, or by other means (e.g. unlock
	// the account in a keystore).
	// 如果钱包需要其他身份验证以签署请求（例如用于解密帐户的密码或用于验证交易的PIN码），则将返回AuthNeededError实例，
	// 其中包含有关用户需要哪些字段或操作的信息。用户可以通过SignDataWithPassphrase或其他方式（例如，在密钥库中解锁帐户）提供所需的详细信息，然后重试。
	SignData(account Account, mimeType string, data []byte) ([]byte, error)

	// SignDataWithPassphrase is identical to SignData, but also takes a password
	// NOTE: there's an chance that an erroneous call might mistake the two strings, and
	// supply password in the mimetype field, or vice versa. Thus, an implementation
	// should never echo the mimetype or return the mimetype in the error-response
	// SignDataWithPassphrase 跟SignData 是一个作用的， 只是带了一个password参数
	// 注意：错误的呼叫有可能会误认两个字符串，并在mimetype字段中提供密码，反之亦然。因此，实现不应在错误响应中回显mimetype或返回mimetype。
	SignDataWithPassphrase(account Account, passphrase, mimeType string, data []byte) ([]byte, error)

	// SignText requests the wallet to sign the hash of a given piece of data, prefixed
	// by the Ethereum prefix scheme
	// It looks up the account specified either solely via its address contained within,
	// or optionally with the aid of any location metadata from the embedded URL field.
	// SignText 请求钱包对给定数据片段的哈希签名，以太坊前缀方案为前缀
	// If the wallet requires additional authentication to sign the request (e.g.
	// a password to decrypt the account, or a PIN code o verify the transaction),
	// an AuthNeededError instance will be returned, containing infos for the user
	// about which fields or actions are needed. The user may retry by providing
	// the needed details via SignHashWithPassphrase, or by other means (e.g. unlock
	// the account in a keystore).
	// 如果钱包需要其他身份验证以签署请求（例如用于解密帐户的密码或用于验证交易的PIN码），则将返回AuthNeededError实例，
	//其中包含有关用户需要哪些字段或操作的信息。用户可以通过SignHashWithPassphrase或其他方式（例如，在密钥库中解锁帐户）提供所需的详细信息来重试。
	// This method should return the signature in 'canonical' format, with v 0 or 1
	// 此方法应以“规范”格式返回签名，其中v 0或1
	SignText(account Account, text []byte) ([]byte, error)

	// SignTextWithPassphrase is identical to Signtext, but also takes a password
	// SignTextWithPassphrase 作用跟Signtext 一样， 只是带有了password的参数
	SignTextWithPassphrase(account Account, passphrase string, hash []byte) ([]byte, error)

	// SignTx requests the wallet to sign the given transaction.
	// SignTx 请求钱包签署给定的交易。
	// It looks up the account specified either solely via its address contained within,
	// or optionally with the aid of any location metadata from the embedded URL field.
	// 它仅通过包含在其中的地址查找指定的帐户， 或可选地借助嵌入式URL字段中的任何位置元数据。
	// If the wallet requires additional authentication to sign the request (e.g.
	// a password to decrypt the account, or a PIN code to verify the transaction),
	// an AuthNeededError instance will be returned, containing infos for the user
	// about which fields or actions are needed. The user may retry by providing
	// the needed details via SignTxWithPassphrase, or by other means (e.g. unlock
	// the account in a keystore).
	// 如果钱包需要其他身份验证以签署请求（例如用于解密帐户的密码或用于验证交易的PIN码），则将返回AuthNeededError实例，
	//其中包含有关用户需要哪些字段或操作的信息。用户可以通过SignTxWithPassphrase提供所需的详细信息或通过其他方式（例如，在密钥库中解锁帐户）重试。
	SignTx(account Account, tx *types.Transaction, chainID *big.Int) (*types.Transaction, error)

	// SignTxWithPassphrase is identical to SignTx, but also takes a password
	// SignTxWithPassphrase 作用等同于 SignTx, 只是携带了一个Password参数
	SignTxWithPassphrase(account Account, passphrase string, tx *types.Transaction, chainID *big.Int) (*types.Transaction, error)
}

// Backend is a "wallet provider" that may contain a batch of accounts they can
// sign transactions with and upon request, do so.
// Backend 是一个“钱包提供商”，其中可能包含一批帐户，他们可以与之签署交易，并应要求进行签署。
type Backend interface {
	// Wallets retrieves the list of wallets the backend is currently aware of.
	// Wallets 检索后端当前知道的钱包列表。
	// The returned wallets are not opened by default. For software HD wallets this
	// means that no base seeds are decrypted, and for hardware wallets that no actual
	// connection is established.
	// 默认情况下，不打开返回的钱包。对于软件HD钱包，这意味着没有基础种子被解密；对于硬件钱包，则没有建立实际的连接。
	// The resulting wallet list will be sorted alphabetically based on its internal
	// URL assigned by the backend. Since wallets (especially hardware) may come and
	// go, the same wallet might appear at a different positions in the list during
	// subsequent retrievals.
	// 产生的钱包列表将根据后端分配的内部URL按字母顺序排序。由于钱包（特别是硬件）可能会来来去去，因此在后续的检索过程中，
	//相同的钱包可能会出现在列表中的不同位置。
	Wallets() []Wallet

	// Subscribe creates an async subscription to receive notifications when the
	// backend detects the arrival or departure of a wallet.
	// Subscribe 创建一个异步订阅以在以下情况下接收通知： 后端检测钱包的到达或离开。
	Subscribe(sink chan<- WalletEvent) event.Subscription
}

// TextHash is a helper function that calculates a hash for the given message that can be
// safely used to calculate a signature from.
// TextHash 是一个帮助函数，它为给定的消息计算哈希值，可以安全地使用该哈希值来计算签名。
// The hash is calulcated as
//   keccak256("\x19Ethereum Signed Message:\n"${message length}${message}).
// 哈希计算为 keccak256("\x19Ethereum Signed Message:\n"${message length}${message}).
// This gives context to the signed message and prevents signing of transactions.
// 这为签名消息提供了上下文，并防止了交易签名。
func TextHash(data []byte) []byte {
	hash, _ := TextAndHash(data)
	return hash
}

// TextAndHash is a helper function that calculates a hash for the given message that can be
// safely used to calculate a signature from.
// TextAndHash 是一个辅助函数，用于为给定消息计算哈希，可以安全地用于计算签名。
// The hash is calulcated as
//   keccak256("\x19Ethereum Signed Message:\n"${message length}${message}).
//
// This gives context to the signed message and prevents signing of transactions.
func TextAndHash(data []byte) ([]byte, string) {
	msg := fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(data), string(data))
	hasher := sha3.NewLegacyKeccak256()
	hasher.Write([]byte(msg))
	return hasher.Sum(nil), msg
}

// WalletEventType represents the different event types that can be fired by
// the wallet subscription subsystem.
// WalletEventType 代表钱包订阅子系统可以触发的不同事件类型。
type WalletEventType int

const (
	// WalletArrived is fired when a new wallet is detected either via USB or via
	// a filesystem event in the keystore.
	// 通过USB或密钥库中的文件系统事件检测到新钱包时，将触发WalletArrived。
	WalletArrived WalletEventType = iota //数值为0

	// WalletOpened is fired when a wallet is successfully opened with the purpose
	// of starting any background processes such as automatic key derivation.
	// WalletOpened 当成功启动任何后台进程（例如自动密钥派生）而成功打开钱包时，会触发。
	WalletOpened //数值为1

	// WalletDropped
	WalletDropped // 数值为2
)

// WalletEvent is an event fired by an account backend when a wallet arrival or
// departure is detected.
// WalletEvent 是检测到钱包到达或离开时由帐户后端触发的事件。
type WalletEvent struct {
	Wallet Wallet          // Wallet instance arrived or departed Wallet的进出
	Kind   WalletEventType // Event type that happened in the system 系统中发生的事件类型
}
