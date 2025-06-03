package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// SmokeTestService provides infrastructure for running the smoke test.
type SmokeTestService struct {
	app *App
}

// SmokeTestingActivated checks if the smoke test should be run.
func (s *SmokeTestService) SmokeTestingActivated() bool {
	if os.Getenv("PM_APP_RUN_SMOKE_TEST") != "1" {
		return false
	}

	// wait for app initialization completed
	<-s.app.initialized
	return true
}

// OnSmokeTestCompleted takes the smoke test result, writes it to disk, and ends the app.
func (s *SmokeTestService) OnSmokeTestCompleted(passed bool, message string) {
	testReportPath := filepath.Join("./app_smoke_test_report.json")
	s.app.log.Info("Writing test report", "path", testReportPath)
	if passed {
		s.app.log.Info("Smoke test passed")
	} else {
		s.app.log.Error("Smoke test failed", "message", message)
	}

	testReport := map[string]any{
		"pass":    passed,
		"message": message,
	}

	// write a test report file
	data, err := json.MarshalIndent(testReport, "", "  ")
	if err != nil {
		s.app.log.Error("Failed to marshal test report", "error", err)
		return
	}
	if err := os.WriteFile(testReportPath, data, 0o644); err != nil {
		s.app.log.Error("Failed to write test report", "error", err)
	}
	runtime.Quit(s.app.ctx)
}
