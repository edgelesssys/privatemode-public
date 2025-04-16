package main

// ConfigurationService provides the configuration to the frontend.
type ConfigurationService struct {
	config *Config
}

// GetConfiguredAPIKey returns the API key set in the config file.
func (s *ConfigurationService) GetConfiguredAPIKey() string {
	return s.config.GetConfiguredAPIKey()
}
