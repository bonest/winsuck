package config

import (
	"encoding/json"
	"fmt"
	"os"
)

const DefaultPort = 9099

type Project struct {
	Source      string   `json:"source"`
	Destination string   `json:"destination"`
	Host        string   `json:"host"`
	Port        int      `json:"port"`
	Workers     int      `json:"workers"`
	Include     []string `json:"include"`
	Exclude     []string `json:"exclude"`
	Update      bool     `json:"update"`
}

func Load(path string) (Project, error) {
	var project Project
	data, err := os.ReadFile(path)
	if err != nil {
		return project, fmt.Errorf("read config %q: %w", path, err)
	}
	if err := json.Unmarshal(data, &project); err != nil {
		return project, fmt.Errorf("parse config %q: %w", path, err)
	}
	return project, nil
}
