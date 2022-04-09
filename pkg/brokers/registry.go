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
