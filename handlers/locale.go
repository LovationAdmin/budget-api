// handlers/locale.go
// The language of a request: the app sends the language of its screen in
// Accept-Language (French by default). See utils/locale.go.

package handlers

import (
	"github.com/LovationAdmin/budget-api/utils"
	"github.com/gin-gonic/gin"
)

// requestLocale is "en" or "fr", from the request's Accept-Language header.
func requestLocale(c *gin.Context) string {
	return utils.LocaleFromAcceptLanguage(c.GetHeader("Accept-Language"))
}

// tr returns the message in the language of the request.
func tr(c *gin.Context, fr, en string) string {
	return say(requestLocale(c), fr, en)
}
