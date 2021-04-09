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
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
)

// DefaultRootDerivationPath is the root path to which custom derivation endpoints
// are appended. As such, the first account will be at m/44'/60'/0'/0, the second
// at m/44'/60'/0'/1, etc.
// DefaultRootDerivationPath 是自定义派生终结点附加到的根路径。
// 这样，第一个帐户将为m / 44'/ 60'/ 0'/ 0，第二个帐户将为m / 44'/ 60'/ 0'/ 1，依此类推。
var DefaultRootDerivationPath = DerivationPath{0x80000000 + 44, 0x80000000 + 60, 0x80000000 + 0, 0}

// DefaultBaseDerivationPath is the base path from which custom derivation endpoints
// are incremented. As such, the first account will be at m/44'/60'/0'/0/0, the second
// at m/44'/60'/0'/0/1, etc.
// DefaultBaseDerivationPath 是自定义派生终结点从其递增的基本路径。
// 这样，第一个帐户将为m / 44'/ 60'/ 0'/ 0/0，第二个帐户将为m / 44'/ 60'/ 0'/ 0/1，依此类推。
var DefaultBaseDerivationPath = DerivationPath{0x80000000 + 44, 0x80000000 + 60, 0x80000000 + 0, 0, 0}

// LegacyLedgerBaseDerivationPath is the legacy base path from which custom derivation
// endpoints are incremented. As such, the first account will be at m/44'/60'/0'/0, the
// second at m/44'/60'/0'/1, etc.
// LegacyLedgerBaseDerivationPath 是从中递增自定义派生终结点的旧版基本路径。
// 这样，第一个帐户将为m / 44'/ 60'/ 0'/ 0，第二个帐户将为m / 44'/ 60'/ 0'/ 1，依此类推。
var LegacyLedgerBaseDerivationPath = DerivationPath{0x80000000 + 44, 0x80000000 + 60, 0x80000000 + 0, 0}

// DerivationPath represents the computer friendly version of a hierarchical
// deterministic wallet account derivaion path.
// DerivationPath 表示分层确定性钱包帐户派生路径的计算机友好版本。
// The BIP-32 spec https://github.com/bitcoin/bips/blob/master/bip-0032.mediawiki
// defines derivation paths to be of the form:
//
//   m / purpose' / coin_type' / account' / change / address_index
//
// The BIP-44 spec https://github.com/bitcoin/bips/blob/master/bip-0044.mediawiki
// defines that the `purpose` be 44' (or 0x8000002C) for crypto currencies, and
// SLIP-44 https://github.com/satoshilabs/slips/blob/master/slip-0044.md assigns
// the `coin_type` 60' (or 0x8000003C) to Ethereum.
//
// The root path for Ethereum is m/44'/60'/0'/0 according to the specification
// from https://github.com/ethereum/EIPs/issues/84, albeit it's not set in stone
// yet whether accounts should increment the last component or the children of
// that. We will go with the simpler approach of incrementing the last component.
// 根据https://github.com/ethereum/EIPs/issues/84的规范，以太坊的根路径为m / 44'/ 60'/ 0'/ 0，尽管它不是一成不变的，
// 但帐户是否应该增加最后一个组成部分或其子元素。我们将采用最简单的方法来增加最后一个分量
type DerivationPath []uint32

// ParseDerivationPath converts a user specified derivation path string to the
// internal binary representation.
// ParseDerivationPath 将用户指定的派生路径字符串转换为内部二进制表达形式

// Full derivation paths need to start with the `m/` prefix, relative derivation
// paths (which will get appended to the default root path) must not have prefixes
// in front of the first element. Whitespace is ignored.
// 完整的派生路径必须以`m /`前缀开头，相对派生路径（将被附加到默认根路径之后）的第一个元素前不得带有前缀。空格被忽略。
// 将
func ParseDerivationPath(path string) (DerivationPath, error) {
	var result DerivationPath

	// Handle absolute or relative paths 处理路径（绝对/相对）
	components := strings.Split(path, "/") // 以“/”为标识进行分割
	switch {
	case len(components) == 0: //如果长度为0  返回异常， empty derivation path
		return nil, errors.New("empty derivation path")

	case strings.TrimSpace(components[0]) == "": //如果第一个元素去空 为"" 则返回异常：ambiguous path: use 'm/' prefix for absolute paths, or no leading '/' for relative ones
		return nil, errors.New("ambiguous path: use 'm/' prefix for absolute paths, or no leading '/' for relative ones")

	case strings.TrimSpace(components[0]) == "m": //如果第一个元素去空 为"m", 则数组重新复制，从下标为1开始copy。
		components = components[1:]

	default: //默认追加，将DefaultRootDerivationPath  追加到result上
		result = append(result, DefaultRootDerivationPath...)
	}
	// All remaining components are relative, append one by one 其余所有组件都是相对的，一个接一个地添加
	if len(components) == 0 { //如果数组长度为0  返回异常empty derivation path
		return nil, errors.New("empty derivation path") // Empty relative paths
	}
	for _, component := range components { //进行遍历
		// Ignore any user added whitespace
		// 忽略任何用户添加的空格
		component = strings.TrimSpace(component) //去空，即上述说的 忽略任何用户添加的空格
		var value uint32

		// Handle hardened paths
		// 处理硬化的路径
		if strings.HasSuffix(component, "'") { //判断元素是否含有'
			value = 0x80000000                                                //如果是 value = 0x80000000
			component = strings.TrimSpace(strings.TrimSuffix(component, "'")) //去掉 '
		}
		// Handle the non hardened component
		// 处理未硬化的组件
		bigval, ok := new(big.Int).SetString(component, 0)
		if !ok { //如果component 不是数字 则会抛出异常
			return nil, fmt.Errorf("invalid component: %s", component)
		}
		max := math.MaxUint32 - value                                    //4294967295 - value
		if bigval.Sign() < 0 || bigval.Cmp(big.NewInt(int64(max))) > 0 { //如果bigval是负数，或者 big大于 max（4294967295 - value）
			if value == 0 { //如果value是0， 返回异常 bigval超过 0 - 4294967295
				return nil, fmt.Errorf("component %v out of allowed range [0, %d]", bigval, max)
			}
			//如果value 不为0，  返回异常： bigval超过 0 - (4294967295-value)
			return nil, fmt.Errorf("component %v out of allowed hardened range [0, %d]", bigval, max)
		}
		value += uint32(bigval.Uint64()) //结果追加

		fmt.Println(value)
		// Append and repeat --- 将路径变为数组形式： eg : m/44'/60'/0'/0/0 -- 转为: [44 60 0 0 0]
		result = append(result, value)
	}
	return result, nil
}

// String implements the stringer interface, converting a binary derivation path
// to its canonical representation.
// String 实现了stringer 接口， 将二进制派生路径转换为其规范表示。
// eg:  [2147483692,2147483708,2147483648,0,0] 转为 m/44'/60'/0'/0/0
func (path DerivationPath) String() string {
	result := "m"
	for _, component := range path {
		var hardened bool
		if component >= 0x80000000 {
			component -= 0x80000000
			hardened = true
		}
		result = fmt.Sprintf("%s/%d", result, component)
		if hardened {
			result += "'"
		}
	}
	return result
}

// MarshalJSON turns a derivation path into its json-serialized string
func (path DerivationPath) MarshalJSON() ([]byte, error) {
	return json.Marshal(path.String())
}

// UnmarshalJSON a json-serialized string back into a derivation path
func (path *DerivationPath) UnmarshalJSON(b []byte) error {
	var dp string
	var err error
	if err = json.Unmarshal(b, &dp); err != nil {
		return err
	}
	*path, err = ParseDerivationPath(dp)
	return err
}

// DefaultIterator creates a BIP-32 path iterator, which progresses by increasing the last component:
// i.e. m/44'/60'/0'/0/0, m/44'/60'/0'/0/1, m/44'/60'/0'/0/2, ... m/44'/60'/0'/0/N.
func DefaultIterator(base DerivationPath) func() DerivationPath {
	path := make(DerivationPath, len(base))
	copy(path[:], base[:])
	// Set it back by one, so the first call gives the first result
	path[len(path)-1]--
	return func() DerivationPath {
		path[len(path)-1]++
		return path
	}
}

// LedgerLiveIterator creates a bip44 path iterator for Ledger Live.
// Ledger Live increments the third component rather than the fifth component
// i.e. m/44'/60'/0'/0/0, m/44'/60'/1'/0/0, m/44'/60'/2'/0/0, ... m/44'/60'/N'/0/0.
func LedgerLiveIterator(base DerivationPath) func() DerivationPath {
	path := make(DerivationPath, len(base))
	copy(path[:], base[:])
	// Set it back by one, so the first call gives the first result
	path[2]--
	return func() DerivationPath {
		// ledgerLivePathIterator iterates on the third component
		path[2]++
		return path
	}
}
