/*
Copyright (c) 2022 RaptorML authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package brokers

import (
	"fmt"
)

//# Brokers registry
var brokers = make(map[string]Broker)

// Register registers a broker
func Register(name string, b Broker) {
	if _, ok := brokers[name]; ok {
		panic(fmt.Errorf("plugin `%s` is already registered", name))
	}
	brokers[name] = b
}

// Get retrieves a broker
func Get(name string) Broker {
	return brokers[name]
}
