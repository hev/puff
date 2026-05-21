package embeddings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

// DefaultEndpoint is the default embedding service endpoint.
const DefaultEndpoint = "http://localhost:8089"

// EmbedRequest is the request payload for the embedding service.
type EmbedRequest struct {
	Text    string `json:"text"`
	ModelID string `json:"model_id"`
}

// EmbedResponse is the response from the embedding service.
type EmbedResponse struct {
	Embedding  []float64 `json:"embedding"`
	Dimensions int       `json:"dimensions"`
	Error      string    `json:"error,omitempty"`
}

// GenerateEmbedding calls the Docker embedding service to generate an embedding.
func GenerateEmbedding(text, modelID string) ([]float32, error) {
	endpoint := os.Getenv("TPUFF_EMBEDDING_URL")
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}

	reqBody := EmbedRequest{
		Text:    text,
		ModelID: modelID,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := http.Post(endpoint+"/embed", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to embedding service at %s: %w\nMake sure the embedding Docker container is running:\n  docker compose -f docker/docker-compose.yml up -d", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding service error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var embedResp EmbedResponse
	if err := json.Unmarshal(respBody, &embedResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if embedResp.Error != "" {
		return nil, fmt.Errorf("embedding error: %s", embedResp.Error)
	}

	// Convert float64 to float32
	result := make([]float32, len(embedResp.Embedding))
	for i, v := range embedResp.Embedding {
		result[i] = float32(v)
	}
	return result, nil
}
