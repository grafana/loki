// +build gofuzz

package logql

func FuzzParseExpr(data []byte) int {
	_, err := ParseExpr(string(data))
	if err != nil {
		return 0
	}
	return 1
}
