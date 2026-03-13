package export

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// TriliumConfig holds connection details for a Trilium Notes ETAPI endpoint.
type TriliumConfig struct {
	URL          string
	Token        string
	ParentNoteID string
}

// UploadSession upserts a note in Trilium: searches by title, updates content if
// found, or creates a new note if not found.
func UploadSession(cfg TriliumConfig, title, markdown string) error {
	client := &http.Client{Timeout: 15 * time.Second}

	// Step 1: Search for an existing note by title.
	query := fmt.Sprintf(`note.title = "%s"`, title)
	encoded := url.QueryEscape(query)
	searchURL := cfg.URL + "/etapi/notes?search=" + encoded

	req, err := http.NewRequest(http.MethodGet, searchURL, nil)
	if err != nil {
		return fmt.Errorf("trilium ETAPI build search request: %w", err)
	}
	req.Header.Set("Authorization", cfg.Token)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("trilium ETAPI GET %s: %w", searchURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("trilium ETAPI %s %s: %s", http.MethodGet, searchURL, resp.Status)
	}

	var searchResult struct {
		Results []struct {
			NoteID string `json:"noteId"`
			Title  string `json:"title"`
		} `json:"results"`
		Count int `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&searchResult); err != nil {
		return fmt.Errorf("trilium ETAPI decode search response: %w", err)
	}

	if len(searchResult.Results) > 0 {
		// Step 2: Update existing note content.
		noteID := searchResult.Results[0].NoteID
		putURL := fmt.Sprintf("%s/etapi/notes/%s/content", cfg.URL, noteID)

		req, err = http.NewRequest(http.MethodPut, putURL, bytes.NewBufferString(markdown))
		if err != nil {
			return fmt.Errorf("trilium ETAPI build PUT request: %w", err)
		}
		req.Header.Set("Authorization", cfg.Token)
		req.Header.Set("Content-Type", "text/plain")

		resp, err = client.Do(req)
		if err != nil {
			return fmt.Errorf("trilium ETAPI PUT %s: %w", putURL, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("trilium ETAPI %s %s: %s", http.MethodPut, putURL, resp.Status)
		}
		return nil
	}

	// Step 3: Create a new note.
	createURL := cfg.URL + "/etapi/create-note"

	body := struct {
		ParentNoteID string `json:"parentNoteId"`
		Title        string `json:"title"`
		Type         string `json:"type"`
		MIME         string `json:"mime"`
		Content      string `json:"content"`
	}{
		ParentNoteID: cfg.ParentNoteID,
		Title:        title,
		Type:         "code",
		MIME:         "text/markdown",
		Content:      markdown,
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("trilium ETAPI marshal create-note body: %w", err)
	}

	req, err = http.NewRequest(http.MethodPost, createURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("trilium ETAPI build POST request: %w", err)
	}
	req.Header.Set("Authorization", cfg.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err = client.Do(req)
	if err != nil {
		return fmt.Errorf("trilium ETAPI POST %s: %w", createURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("trilium ETAPI %s %s: %s", http.MethodPost, createURL, resp.Status)
	}
	return nil
}
