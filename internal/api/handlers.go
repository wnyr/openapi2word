package api

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wnyr/openapi2word/internal/docgen"
	"github.com/wnyr/openapi2word/internal/model"
	"github.com/wnyr/openapi2word/internal/parser"
)

type ParseRequest struct {
	URL string `json:"url"`
}

type ParseResponse struct {
	Doc model.APIDocument `json:"doc"`
}

type GenerateResponse struct {
	Filename string `json:"filename"`
}

func RegisterRoutes(r *gin.Engine) {
	r.POST("/api/parse", handleParse)
	r.POST("/api/generate", handleGenerate)
}

func handleParse(c *gin.Context) {
	data, err := readParsePayload(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	doc, err := parser.ParseDocument(data)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ParseResponse{Doc: *doc})
}

func handleGenerate(c *gin.Context) {
	var req model.GenerateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	selected := map[string]bool{}
	for _, id := range req.EndpointIDs {
		selected[id] = true
	}
	bytes, err := docgen.BuildDocx(req.Doc, req.Meta, selected)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	filename := "api.docx"
	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.wordprocessingml.document", bytes)
}

func readParsePayload(c *gin.Context) ([]byte, error) {
	file, err := c.FormFile("file")
	if err == nil && file != nil {
		f, err := file.Open()
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return io.ReadAll(f)
	}

	var req ParseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		return nil, fmt.Errorf("file or url required")
	}
	if strings.TrimSpace(req.URL) == "" {
		return nil, fmt.Errorf("url required")
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(req.URL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch failed: %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}
