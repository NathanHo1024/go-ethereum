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
	"strings"
)

// URL represents the canonical identification URL of a wallet or account.
// URL 代表钱包或帐户的规范标识URL。
// It is a simplified version of url.URL, with the important limitations (which
// are considered features here) that it contains value-copyable components only,
// as well as that it doesn't do any URL encoding/decoding of special characters.
// 它是url.URL的简化版本，具有重要的限制（在这里被认为是功能），它仅包含可复制值，并且不对特殊字符进行任何URL编码/解码
// The former is important to allow an account to be copied without leaving live
// references to the original version, whereas the latter is important to ensure
// one single canonical form opposed to many allowed ones by the RFC 3986 spec.
// 前者对于允许在不保留原始版本的实时引用的情况下复制帐户很重要，而后者对于确保一种规范形式（与RFC 3986规范所允许的许多形式相反）很重要。
// As such, these URLs should not be used outside of the scope of an Ethereum
// wallet or account.
// 因此，不应在以太坊钱包或账户范围之外使用这些URL。
type URL struct {
	Scheme string // Protocol scheme to identify a capable account backend 识别有能力的帐户后端的协议方案
	Path   string // Path for the backend to identify a unique entity  Path 后端识别唯一实体
}

// parseURL converts a user supplied URL into the accounts specific structure.
// parseURL 将用户提供的url转换为账户特定的结构
func parseURL(url string) (URL, error) {
	parts := strings.Split(url, "://")     //分离数组
	if len(parts) != 2 || parts[0] == "" { //如果分离的数组长度部位2，或者第一part== ""
		return URL{}, errors.New("protocol scheme missing") //抛异常 协议丢失
	}
	return URL{ //否则返回结果
		Scheme: parts[0],
		Path:   parts[1],
	}, nil
}

// String implements the stringer interface.
// String 实现了 stringer接口
func (u URL) String() string {
	if u.Scheme != "" { //当前的URL 协议不== ""
		return fmt.Sprintf("%s://%s", u.Scheme, u.Path)
	}
	return u.Path //否则返回路径
}

// TerminalString implements the log.TerminalStringer interface.
// TerminalString 实现log.TerminalStringer接口
func (u URL) TerminalString() string {
	url := u.String()  //当前URL获取Path
	if len(url) > 32 { //如果Path大于32，后面补...
		return url[:31] + "…"
	}
	return url
}

// MarshalJSON implements the json.Marshaller interface.
// MarshalJSON 实现了 json.Marshalle 接口
func (u URL) MarshalJSON() ([]byte, error) {
	return json.Marshal(u.String())
}

// UnmarshalJSON parses url.
// UnmarshalJSON 解析url
func (u *URL) UnmarshalJSON(input []byte) error {
	var textURL string
	err := json.Unmarshal(input, &textURL)
	if err != nil {
		return err
	}
	url, err := parseURL(textURL)
	if err != nil {
		return err
	}
	u.Scheme = url.Scheme
	u.Path = url.Path
	return nil
}

// Cmp compares x and y and returns:
//
//   -1 if x <  y
//    0 if x == y
//   +1 if x >  y
//
func (u URL) Cmp(url URL) int {
	if u.Scheme == url.Scheme {
		return strings.Compare(u.Path, url.Path)
	}
	return strings.Compare(u.Scheme, url.Scheme)
}
