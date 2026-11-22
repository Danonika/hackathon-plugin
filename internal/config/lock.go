package config

import (
	"time"

	"hackathonkit.local/agentkit/internal/filelock"
)

func acquireLock(path string) (func(), error) { return filelock.Acquire(path, 5*time.Second) }
