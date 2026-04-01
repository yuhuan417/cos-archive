package main

import (
	"encoding/json"
	"io"
	"os"
)

// Config struct
type Config struct {
	BasePath   string
	FilePaths  []string
	SkipList   []string
	Threads    int
	WorkingDir string
	TargetDir  string
	Index      string
	Port       string
	COS        COSConfig
}

// COSConfig struct
type COSConfig struct {
	URL         string
	ID          string
	Key         string
	ChunkPrefix string `json:"Prefix"`
	Class       string
	Retries     int
}

func getConfig(configFileName string) (config Config) {
	// default value
	config.Threads = 4
	config.Index = "meta.json"
	config.COS.ChunkPrefix = "data/"
	config.COS.Class = "DEEP_ARCHIVE"
	config.COS.Retries = 1

	jsonFile, err := os.Open(configFileName)

	if err != nil {
		Fatal("Open config file error:", err)
	}

	defer jsonFile.Close()

	byteValue, err := io.ReadAll(jsonFile)
	if err != nil {
		Fatal("Read config file error:", err)
	}
	if err := json.Unmarshal(byteValue, &config); err != nil {
		Fatal("Parse config file error:", err)
	}

	return
}

func validateConfig(action string, config Config) {
	switch action {
	case "backup":
		validateWorkingDir(config)
		validateFilePaths(config)
		validateThreads(config)
		validateCOSConfig(config)
	case "restore":
		validateCOSConfig(config)
	case "download":
		validateWorkingDir(config)
		validateTargetDir(config)
		validateCOSConfig(config)
	case "browse":
		validateTargetDir(config)
		if config.Port == "" {
			Fatal("Empty port.")
		}
	case "link":
		validateTargetDir(config)
	case "fsck":
		validateWorkingDir(config)
		validateCOSConfig(config)
	case "verify":
		validateWorkingDir(config)
		validateFilePaths(config)
		validateCOSConfig(config)
	case "":
		Fatal("Empty action.")
	default:
		Fatal("Unknown action:", action)
	}
}

func validateWorkingDir(config Config) {
	if config.WorkingDir == "" {
		Fatal("Empty working dir.")
	}
}

func validateTargetDir(config Config) {
	if config.TargetDir == "" {
		Fatal("Empty target dir.")
	}
}

func validateFilePaths(config Config) {
	if len(config.FilePaths) == 0 {
		Fatal("Empty file paths.")
	}
}

func validateThreads(config Config) {
	if config.Threads <= 0 {
		Fatal("Invalid threads:", config.Threads)
	}
}

func validateCOSConfig(config Config) {
	if config.COS.URL == "" {
		Fatal("Empty COS URL.")
	}
	if config.COS.ID == "" {
		Fatal("Empty COS ID.")
	}
	if config.COS.Key == "" {
		Fatal("Empty COS key.")
	}
}
