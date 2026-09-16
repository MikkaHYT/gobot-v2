package ripper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gobot/internal/helpers"
	"gobot/internal/logger"
)

type ExternalUploadResult struct {
	URL       string
	DirectURL string
	PageURL   string
	Provider  string
	ExpiresAt string
}

type ExternalUploader interface {
	Upload(ctx context.Context, filePath string) (*ExternalUploadResult, error)
}

type StorageToUploader struct {
	client  *http.Client
	apiKey  string
	baseURL string
}

func NewStorageToUploader(apiKey string) *StorageToUploader {
	return &StorageToUploader{
		client: helpers.NewSafeHTTPClient(10 * time.Minute),
		apiKey: strings.TrimSpace(apiKey),
	}
}

func (s *StorageToUploader) getBaseURL() string {
	if s.baseURL != "" {
		return strings.TrimRight(s.baseURL, "/")
	}
	return "https://storage.to/api"
}

type storageToInitResponse struct {
	Success     bool              `json:"success"`
	Type        string            `json:"type"`
	UploadURL   string            `json:"upload_url"`
	UploadID    string            `json:"upload_id"`
	R2Key       string            `json:"r2_key"`
	PartSize    int64             `json:"part_size"`
	TotalParts  int               `json:"total_parts"`
	InitialURLs map[string]string `json:"initial_urls"`
}

type storageToPartRecord struct {
	PartNumber int    `json:"partNumber"`
	ETag       string `json:"etag"`
}

type storageToCompleteRequest struct {
	UploadID string                `json:"upload_id"`
	Parts    []storageToPartRecord `json:"parts"`
}

type storageToCompleteResponse struct {
	Success bool   `json:"success"`
	R2Key   string `json:"r2_key"`
	Error   string `json:"error"`
}

type storageToConfirmResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
	File    struct {
		ID        string `json:"id"`
		URL       string `json:"url"`
		Filename  string `json:"filename"`
		ExpiresAt string `json:"expires_at"`
	} `json:"file"`
}

func (s *StorageToUploader) Upload(ctx context.Context, filePath string) (*ExternalUploadResult, error) {
	fi, err := os.Stat(filePath)
	if err != nil {
		return nil, err
	}

	filename := filepath.Base(filePath)
	contentType := mime.TypeByExtension(filepath.Ext(filePath))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	initPayload := map[string]any{
		"filename":     filename,
		"content_type": contentType,
		"size":         fi.Size(),
	}
	initBodyBytes, _ := json.Marshal(initPayload)

	initReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.getBaseURL()+"/upload/init", bytes.NewReader(initBodyBytes))
	if err != nil {
		return nil, err
	}
	initReq.Header.Set("Content-Type", "application/json")
	initReq.Header.Set("User-Agent", helpers.GetUserAgent())
	if s.apiKey != "" {
		initReq.Header.Set("Authorization", "Bearer "+s.apiKey)
	}

	initResp, err := s.client.Do(initReq)
	if err != nil {
		return nil, fmt.Errorf("storage.to init failed: %w", err)
	}
	defer initResp.Body.Close()

	if initResp.StatusCode < 200 || initResp.StatusCode >= 300 {
		respSnippet, _ := io.ReadAll(io.LimitReader(initResp.Body, 512))
		return nil, fmt.Errorf("storage.to init returned status %d: %s", initResp.StatusCode, string(respSnippet))
	}

	var initRes storageToInitResponse
	if err := json.NewDecoder(initResp.Body).Decode(&initRes); err != nil {
		return nil, fmt.Errorf("failed to decode storage.to init response: %w", err)
	}

	var parts []storageToPartRecord

	if initRes.Type == "multipart" && len(initRes.InitialURLs) > 0 {
		file, err := os.Open(filePath)
		if err != nil {
			return nil, err
		}
		defer file.Close()

		partSize := initRes.PartSize
		if partSize <= 0 {
			partSize = 33554432
		}

		partURLs := initRes.InitialURLs
		if partURLs == nil {
			partURLs = make(map[string]string)
		}

		var missingParts []int
		for partNum := 1; partNum <= initRes.TotalParts; partNum++ {
			if _, ok := partURLs[strconv.Itoa(partNum)]; !ok {
				missingParts = append(missingParts, partNum)
			}
		}
		if len(missingParts) > 0 {
			fetched, err := s.fetchPartURLs(ctx, initRes.UploadID, missingParts)
			if err != nil {
				return nil, err
			}
			for k, v := range fetched {
				partURLs[k] = v
			}
		}

		for partNum := 1; partNum <= initRes.TotalParts; partNum++ {
			partURL, ok := partURLs[strconv.Itoa(partNum)]
			if !ok {
				return nil, fmt.Errorf("missing upload url for part %d", partNum)
			}

			partBuffer := make([]byte, partSize)
			bytesRead, readErr := io.ReadFull(file, partBuffer)
			if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
				return nil, fmt.Errorf("failed reading part %d: %w", partNum, readErr)
			}
			partData := partBuffer[:bytesRead]

			putReq, err := http.NewRequestWithContext(ctx, http.MethodPut, partURL, bytes.NewReader(partData))
			if err != nil {
				return nil, err
			}
			putReq.Header.Set("Content-Type", contentType)

			putResp, err := s.client.Do(putReq)
			if err != nil {
				return nil, fmt.Errorf("failed uploading part %d: %w", partNum, err)
			}
			_ = putResp.Body.Close()

			if putResp.StatusCode < 200 || putResp.StatusCode >= 300 {
				return nil, fmt.Errorf("uploading part %d failed with status %d", partNum, putResp.StatusCode)
			}

			etag := putResp.Header.Get("ETag")
			parts = append(parts, storageToPartRecord{
				PartNumber: partNum,
				ETag:       etag,
			})
		}

		completePayload := storageToCompleteRequest{
			UploadID: initRes.UploadID,
			Parts:    parts,
		}
		completeBodyBytes, _ := json.Marshal(completePayload)

		completeReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.getBaseURL()+"/upload/complete-multipart", bytes.NewReader(completeBodyBytes))
		if err != nil {
			return nil, err
		}
		completeReq.Header.Set("Content-Type", "application/json")
		completeReq.Header.Set("User-Agent", helpers.GetUserAgent())
		if s.apiKey != "" {
			completeReq.Header.Set("Authorization", "Bearer "+s.apiKey)
		}

		completeResp, err := s.client.Do(completeReq)
		if err != nil {
			return nil, fmt.Errorf("storage.to complete-multipart failed: %w", err)
		}
		defer completeResp.Body.Close()

		if completeResp.StatusCode < 200 || completeResp.StatusCode >= 300 {
			respSnippet, _ := io.ReadAll(io.LimitReader(completeResp.Body, 512))
			return nil, fmt.Errorf("storage.to complete-multipart returned status %d: %s", completeResp.StatusCode, string(respSnippet))
		}

		var compRes storageToCompleteResponse
		if err := json.NewDecoder(completeResp.Body).Decode(&compRes); err != nil {
			return nil, fmt.Errorf("failed to decode storage.to complete-multipart response: %w", err)
		}
		if !compRes.Success {
			return nil, fmt.Errorf("storage.to complete-multipart unsuccessful: %s", compRes.Error)
		}
		if compRes.R2Key != "" {
			initRes.R2Key = compRes.R2Key
		}
	} else {
		file, err := os.Open(filePath)
		if err != nil {
			return nil, err
		}
		defer file.Close()

		putReq, err := http.NewRequestWithContext(ctx, http.MethodPut, initRes.UploadURL, file)
		if err != nil {
			return nil, err
		}
		putReq.Header.Set("Content-Type", contentType)
		putReq.ContentLength = fi.Size()

		putResp, err := s.client.Do(putReq)
		if err != nil {
			return nil, fmt.Errorf("storage.to direct upload failed: %w", err)
		}
		defer putResp.Body.Close()

		if putResp.StatusCode < 200 || putResp.StatusCode >= 300 {
			respSnippet, _ := io.ReadAll(io.LimitReader(putResp.Body, 512))
			return nil, fmt.Errorf("storage.to direct upload returned status %d: %s", putResp.StatusCode, string(respSnippet))
		}
	}

	confirmPayload := map[string]any{
		"r2_key":       initRes.R2Key,
		"filename":     filename,
		"content_type": contentType,
		"size":         fi.Size(),
	}
	confirmBodyBytes, _ := json.Marshal(confirmPayload)

	confirmReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.getBaseURL()+"/upload/confirm", bytes.NewReader(confirmBodyBytes))
	if err != nil {
		return nil, err
	}
	confirmReq.Header.Set("Content-Type", "application/json")
	confirmReq.Header.Set("User-Agent", helpers.GetUserAgent())
	if s.apiKey != "" {
		confirmReq.Header.Set("Authorization", "Bearer "+s.apiKey)
	}

	confirmResp, err := s.client.Do(confirmReq)
	if err != nil {
		return nil, fmt.Errorf("storage.to confirm failed: %w", err)
	}
	defer confirmResp.Body.Close()

	if confirmResp.StatusCode < 200 || confirmResp.StatusCode >= 300 {
		respSnippet, _ := io.ReadAll(io.LimitReader(confirmResp.Body, 512))
		return nil, fmt.Errorf("storage.to confirm returned status %d: %s", confirmResp.StatusCode, string(respSnippet))
	}

	var confirmRes storageToConfirmResponse
	if err := json.NewDecoder(confirmResp.Body).Decode(&confirmRes); err != nil {
		return nil, fmt.Errorf("failed to decode storage.to confirm response: %w", err)
	}

	if !confirmRes.Success || confirmRes.File.URL == "" {
		errMsg := confirmRes.Error
		if errMsg == "" {
			errMsg = "storage.to returned empty download url"
		}
		return nil, errors.New(errMsg)
	}

	var directURL string
	if confirmRes.File.ID != "" {
		pageURL := confirmRes.File.URL
		if s.baseURL != "" && !strings.HasPrefix(pageURL, s.getHostRoot()) {
			pageURL = fmt.Sprintf("%s/%s", s.getHostRoot(), confirmRes.File.ID)
		}
		directURL = s.resolveDirectDownloadURL(ctx, confirmRes.File.ID, pageURL)
	}

	primaryURL := confirmRes.File.URL
	if directURL != "" {
		primaryURL = directURL
	}

	return &ExternalUploadResult{
		URL:       primaryURL,
		DirectURL: directURL,
		PageURL:   confirmRes.File.URL,
		Provider:  "storage.to",
		ExpiresAt: confirmRes.File.ExpiresAt,
	}, nil
}

func (s *StorageToUploader) getHostRoot() string {
	base := s.getBaseURL()
	return strings.TrimSuffix(base, "/api")
}

func (s *StorageToUploader) fetchPartURLs(ctx context.Context, uploadID string, partNumbers []int) (map[string]string, error) {
	payload := map[string]any{
		"upload_id":    uploadID,
		"part_numbers": partNumbers,
	}
	bodyBytes, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.getBaseURL()+"/upload/parts", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", helpers.GetUserAgent())
	if s.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.apiKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("storage.to parts request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("storage.to parts returned status %d", resp.StatusCode)
	}

	var partsRes struct {
		Success bool              `json:"success"`
		URLs    map[string]string `json:"urls"`
		Error   string            `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&partsRes); err != nil {
		return nil, fmt.Errorf("failed to decode storage.to parts response: %w", err)
	}
	if !partsRes.Success {
		return nil, fmt.Errorf("storage.to parts unsuccessful: %s", partsRes.Error)
	}
	return partsRes.URLs, nil
}

var mintProofRegex = regexp.MustCompile(`mint_proof[^\w]+([0-9]+\.[0-9a-fA-F]+)`)

func (s *StorageToUploader) resolveDirectDownloadURL(ctx context.Context, fileID, pageURL string) string {
	pageReq, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return ""
	}
	pageReq.Header.Set("User-Agent", helpers.GetUserAgent())
	pageResp, err := s.client.Do(pageReq)
	if err != nil {
		return ""
	}
	defer pageResp.Body.Close()

	if pageResp.StatusCode != http.StatusOK {
		return ""
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(pageResp.Body, 128*1024))
	if err != nil {
		return ""
	}

	matches := mintProofRegex.FindSubmatch(bodyBytes)
	if len(matches) < 2 {
		return ""
	}
	mintProof := string(matches[1])

	dlURL := fmt.Sprintf("%s/%s/download", s.getHostRoot(), fileID)
	dlReq, err := http.NewRequestWithContext(ctx, http.MethodGet, dlURL, nil)
	if err != nil {
		return ""
	}
	dlReq.Header.Set("User-Agent", helpers.GetUserAgent())
	dlReq.Header.Set("Accept", "application/json")
	dlReq.Header.Set("x-mint-proof", mintProof)
	if s.apiKey != "" {
		dlReq.Header.Set("Authorization", "Bearer "+s.apiKey)
	}

	dlResp, err := s.client.Do(dlReq)
	if err != nil {
		return ""
	}
	defer dlResp.Body.Close()

	if dlResp.StatusCode != http.StatusOK {
		return ""
	}

	var dlRes struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(dlResp.Body).Decode(&dlRes); err != nil {
		return ""
	}

	return dlRes.URL
}

type LitterboxUploader struct {
	client *http.Client
	time   string
	apiURL string
}

func NewLitterboxUploader(retention string) *LitterboxUploader {
	if retention == "" {
		retention = "24h"
	}
	return &LitterboxUploader{
		client: helpers.NewSafeHTTPClient(10 * time.Minute),
		time:   retention,
	}
}

func (l *LitterboxUploader) getAPIURL() string {
	if l.apiURL != "" {
		return l.apiURL
	}
	return "https://litterbox.catbox.moe/resources/internals/api.php"
}

func (l *LitterboxUploader) Upload(ctx context.Context, filePath string) (*ExternalUploadResult, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	pipeReader, pipeWriter := io.Pipe()
	writer := multipart.NewWriter(pipeWriter)

	helpers.Spawn(func() {
		var closeErr error
		defer func() {
			_ = writer.Close()
			_ = pipeWriter.CloseWithError(closeErr)
		}()

		if err := writer.WriteField("reqtype", "fileupload"); err != nil {
			closeErr = err
			return
		}
		if err := writer.WriteField("time", l.time); err != nil {
			closeErr = err
			return
		}
		part, err := writer.CreateFormFile("fileToUpload", filepath.Base(filePath))
		if err != nil {
			closeErr = err
			return
		}
		if _, err := io.Copy(part, file); err != nil {
			closeErr = err
			return
		}
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.getAPIURL(), pipeReader)
	if err != nil {
		_ = pipeReader.Close()
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", helpers.GetUserAgent())

	resp, err := l.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("litterbox request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err != nil {
		return nil, err
	}
	resultURL := strings.TrimSpace(string(respBytes))
	if !strings.HasPrefix(resultURL, "http") {
		return nil, fmt.Errorf("litterbox upload failed: %s", resultURL)
	}

	return &ExternalUploadResult{
		URL:       resultURL,
		Provider:  "Litterbox",
		ExpiresAt: l.time,
	}, nil
}

type CompositeUploader struct {
	primary  ExternalUploader
	fallback ExternalUploader
}

func NewCompositeUploader(primary, fallback ExternalUploader) *CompositeUploader {
	return &CompositeUploader{
		primary:  primary,
		fallback: fallback,
	}
}

func (c *CompositeUploader) Upload(ctx context.Context, filePath string) (*ExternalUploadResult, error) {
	if c.primary != nil {
		res, err := c.primary.Upload(ctx, filePath)
		if err == nil && res != nil && res.URL != "" {
			return res, nil
		}
		logger.Warnf("[RIPPER] Primary uploader failed: %v, falling back...", err)
	}

	if c.fallback != nil {
		return c.fallback.Upload(ctx, filePath)
	}

	return nil, errors.New("all upload providers failed")
}

func NewDefaultExternalUploader(storageToKey string) ExternalUploader {
	return NewCompositeUploader(
		NewStorageToUploader(storageToKey),
		NewLitterboxUploader("24h"),
	)
}
