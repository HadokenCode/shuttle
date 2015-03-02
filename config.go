package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"sync"

	"github.com/litl/shuttle/client"
	"github.com/litl/shuttle/log"
)

func loadConfig() {
	for _, cfgPath := range []string{stateConfig, defaultConfig} {
		if cfgPath == "" {
			continue
		}

		cfgData, err := ioutil.ReadFile(cfgPath)
		if err != nil {
			log.Warnln("Error reading config:", err)
			continue
		}

		var cfg client.Config
		err = json.Unmarshal(cfgData, &cfg)
		if err != nil {
			log.Warnln("Config error:", err)
			continue
		}
		log.Debug("Loaded config from:", cfgPath)

		if err := Registry.UpdateConfig(cfg); err != nil {
			log.Printf("Unable to load config: error: %s", err)
		}
	}
}

// protects the state config file
var configMutex sync.Mutex

func writeStateConfig() {
	configMutex.Lock()
	defer configMutex.Unlock()

	if stateConfig == "" {
		log.Debug("No state file. Not saving changes")
		return
	}

	cfg := marshal(Registry.Config())
	if len(cfg) == 0 {
		return
	}

	lastCfg, _ := ioutil.ReadFile(stateConfig)
	if bytes.Equal(cfg, lastCfg) {
		log.Println("No change in config")
		return
	}

	// We should probably write a temp file and mv for atomic update.
	err := ioutil.WriteFile(stateConfig, cfg, 0644)
	if err != nil {
		log.Println("Error saving config state:", err)
	}
}
