package export

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

// TriliumConfig holds connection details for a Trilium Notes ETAPI endpoint.
type TriliumConfig struct {
	URL          string
	Token        string
	ParentNoteID string
}

func markdownToHTML(md string) (string, error) {
	gm := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Table,
			extension.Strikethrough,
			extension.TaskList,
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			html.WithHardWraps(),
			html.WithXHTML(),
			html.WithUnsafe(), // allow raw HTML in markdown (e.g. code blocks with special chars)
		),
	)
	var buf bytes.Buffer
	if err := gm.Convert([]byte(md), &buf); err != nil {
		return "", fmt.Errorf("markdown→HTML conversion: %w", err)
	}
	return buf.String(), nil
}

// UploadSession upserts a note in Trilium: searches by title, updates content if
// found, or creates a new note if not found.
func UploadSession(cfg TriliumConfig, title, markdown string) error {
	htmlContent, err := markdownToHTML(markdown)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 15 * time.Second}

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
		noteID := searchResult.Results[0].NoteID
		putURL := fmt.Sprintf("%s/etapi/notes/%s/content", cfg.URL, noteID)

		req, err = http.NewRequest(http.MethodPut, putURL, bytes.NewBufferString(htmlContent))
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
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("trilium ETAPI %s %s: %s — %s", http.MethodPut, putURL, resp.Status, body)
		}
		return nil
	}

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
		Type:         "text",
		MIME:         "text/html",
		Content:      htmlContent,
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
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("trilium ETAPI %s %s: %s — %s", http.MethodPost, createURL, resp.Status, body)
	}
	return nil
}
