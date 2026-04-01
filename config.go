package main

import (
	"encoding/json"
	"fmt"
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
	MountPoint string
	Index      string
	Port       string
	RestoreQPS int
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

func getConfig(configFileName string) (Config, error) {
	config := Config{}
	config.Threads = 4
	config.Index = "meta.json"
	config.RestoreQPS = 90
	config.COS.ChunkPrefix = "data/"
	config.COS.Class = "DEEP_ARCHIVE"
	config.COS.Retries = 1

	jsonFile, err := os.Open(configFileName)
	if err != nil {
		return config, fmt.Errorf("%w: open config file: %v", ErrConfigInvalid, err)
	}
	defer jsonFile.Close()

	byteValue, err := io.ReadAll(jsonFile)
	if err != nil {
		return config, fmt.Errorf("%w: read config file: %v", ErrConfigInvalid, err)
	}
	if err := json.Unmarshal(byteValue, &config); err != nil {
		return config, fmt.Errorf("%w: parse config file: %v", ErrConfigInvalid, err)
	}

	return config, nil
}

func applyRuntimeOverrides(config Config, targetDir string, mountPoint string) Config {
	if targetDir != "" {
		config.TargetDir = targetDir
	}
	if mountPoint != "" {
		config.MountPoint = mountPoint
	}
	return config
}

func validateConfig(action string, config Config) error {
	switch action {
	case "backup":
		if err := validateWorkingDir(config); err != nil {
			return err
		}
		if err := validateFilePaths(config); err != nil {
			return err
		}
		if err := validateThreads(config); err != nil {
			return err
		}
		return validateCOSConfig(config)
	case "restore":
		return validateCOSConfig(config)
	case "download":
		if err := validateWorkingDir(config); err != nil {
			return err
		}
		if err := validateTargetDir(config); err != nil {
			return err
		}
		return validateCOSConfig(config)
	case "browse":
		if err := validateTargetDir(config); err != nil {
			return err
		}
		if config.Port == "" {
			return fmt.Errorf("%w: empty port", ErrConfigInvalid)
		}
		return nil
	case "link":
		return validateTargetDir(config)
	case "mount":
		if err := validateTargetDir(config); err != nil {
			return err
		}
		if config.MountPoint == "" {
			return fmt.Errorf("%w: empty mount point", ErrConfigInvalid)
		}
		return nil
	case "fsck":
		if err := validateWorkingDir(config); err != nil {
			return err
		}
		return validateCOSConfig(config)
	case "verify":
		if err := validateWorkingDir(config); err != nil {
			return err
		}
		if err := validateFilePaths(config); err != nil {
			return err
		}
		return validateCOSConfig(config)
	case "":
		return fmt.Errorf("%w: empty action", ErrUnknownAction)
	default:
		return fmt.Errorf("%w: %s", ErrUnknownAction, action)
	}
}

func validateWorkingDir(config Config) error {
	if config.WorkingDir == "" {
		return fmt.Errorf("%w: empty working dir", ErrConfigInvalid)
	}
	return nil
}

func validateTargetDir(config Config) error {
	if config.TargetDir == "" {
		return fmt.Errorf("%w: empty target dir", ErrConfigInvalid)
	}
	return nil
}

func validateFilePaths(config Config) error {
	if len(config.FilePaths) == 0 {
		return fmt.Errorf("%w: empty file paths", ErrConfigInvalid)
	}
	return nil
}

func validateThreads(config Config) error {
	if config.Threads <= 0 {
		return fmt.Errorf("%w: invalid threads %d", ErrConfigInvalid, config.Threads)
	}
	return nil
}

func validateCOSConfig(config Config) error {
	if config.COS.URL == "" {
		return fmt.Errorf("%w: empty COS URL", ErrConfigInvalid)
	}
	if config.COS.ID == "" {
		return fmt.Errorf("%w: empty COS ID", ErrConfigInvalid)
	}
	if config.COS.Key == "" {
		return fmt.Errorf("%w: empty COS key", ErrConfigInvalid)
	}
	return nil
}
