package api

import (
	_ "embed"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed specialist-booking.openapi.yaml
var openapi []byte

func HandleOpenApi(c *gin.Context) {
	c.Data(http.StatusOK, "application/yaml", openapi)
}
