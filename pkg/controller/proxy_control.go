// Copyright 2018 deepfabric, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License.

package controller

import (
	"net/http"
)

// ProxyControlInterface is the interface that knows how to manage proxy peers
type ProxyControlInterface interface {
	// GetHealth returns proxy's health info
}

// defaultProxyControl is default implementation of ProxyControlInterface.
type defaultProxyControl struct {
	httpClient *http.Client
}

// NewDefaultProxyControl returns a defaultProxyControl instance
func NewDefaultProxyControl() ProxyControlInterface {
	httpClient := &http.Client{Timeout: timeout}
	return &defaultProxyControl{httpClient: httpClient}
}

// FakeProxyControl is a fake implementation of ProxyControlInterface.
type FakeProxyControl struct {
}

// NewFakeProxyControl returns a FakeProxyControl instance
func NewFakeProxyControl() *FakeProxyControl {
	return &FakeProxyControl{}
}
