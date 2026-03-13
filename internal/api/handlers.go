package api

import (
	"io"
	"net/http"
	"strings"

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
	var data []byte

	file, err := c.FormFile("file")
	if err == nil && file != nil {
		f, err := file.Open()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		defer f.Close()
		data, err = io.ReadAll(f)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	} else {
		var req ParseRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "file or url required"})
			return
		}
		if strings.TrimSpace(req.URL) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "url required"})
			return
		}
		resp, err := http.Get(req.URL)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		defer resp.Body.Close()
		data, err = io.ReadAll(resp.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
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
