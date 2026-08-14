package handler

import "github.com/gin-gonic/gin"

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorResponse struct {
	Error     errorDetail `json:"error"`
	RequestID string      `json:"request_id"`
}

func writeAPIError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, errorResponse{
		Error: errorDetail{
			Code:    code,
			Message: message,
		},
		RequestID: requestIDFromContext(c),
	})
}
