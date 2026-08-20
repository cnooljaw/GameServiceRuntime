package nhsk

import (
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func replayUTF8OrGBK(value string) string {
	if utf8.ValidString(value) {
		return value
	}
	decoded, err := simplifiedchinese.GBK.NewDecoder().Bytes([]byte(value))
	if err != nil {
		return value
	}
	return string(decoded)
}
