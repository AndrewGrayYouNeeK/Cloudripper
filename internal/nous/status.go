package nous

import (
	"context"
	"errors"
	"time"
)

// Status is what the desktop app shows on the Nous screen.
type Status struct {
	Enabled       bool     `json:"enabled"`
	EnabledAt     string   `json:"enabled_at,omitempty"`
	Model         string   `json:"model,omitempty"`
	Endpoint      string   `json:"endpoint"`
	OllamaRunning bool     `json:"ollama_running"`
	Models        []string `json:"models"`
	Message       string   `json:"message"`
	WarningTitle  string   `json:"warning_title"`
	Warning       []string `json:"warning"`
	Phrase        string   `json:"phrase"`
	Slogan        string   `json:"slogan"`
}

// CurrentStatus reads the opt-in and, only when Nous is on, asks local
// Ollama which models are installed.
func CurrentStatus(ctx context.Context, path string) Status {
	st := Status{
		Endpoint:     Endpoint(),
		Models:       []string{},
		WarningTitle: WarningTitle,
		Warning:      WarningLines(),
		Phrase:       ConfirmPhrase,
		Slogan:       Slogan,
	}
	settings, err := Load(path)
	if err != nil {
		st.Message = err.Error()
		return st
	}
	st.Enabled = settings.Enabled
	st.EnabledAt = settings.EnabledAt
	if !settings.Enabled {
		st.Message = "Nous is off. Nothing is read until you turn it on."
		return st
	}
	client, err := NewClient(st.Endpoint)
	if err != nil {
		st.Message = err.Error()
		return st
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	models, err := client.Models(ctx)
	if err != nil {
		st.Message = err.Error()
		return st
	}
	st.OllamaRunning = true
	st.Models = models
	st.Model = PickModel(models, settings.Model)
	if st.Model == "" {
		st.Message = ErrNoModel.Error()
		return st
	}
	st.Message = "Nous is on and uses " + st.Model + " on this computer. Nothing leaves this machine."
	return st
}

// EnableTyped is the desktop path: the typed text is checked here too, so a
// UI bug cannot turn Nous on without the phrase.
func EnableTyped(ctx context.Context, path, typed, model string) (Status, error) {
	if _, err := Enable(path, typed, model, time.Now()); err != nil {
		if errors.Is(err, ErrNotConfirmed) {
			return CurrentStatus(ctx, path), err
		}
		return Status{}, err
	}
	return CurrentStatus(ctx, path), nil
}
