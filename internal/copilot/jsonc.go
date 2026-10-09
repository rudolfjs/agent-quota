package copilot

// stripJSONC converts JSON with comments (// and /* */) and trailing commas
// into plain JSON. String contents are copied verbatim. Malformed input is
// passed through for json.Unmarshal to reject.
func stripJSONC(data []byte) []byte {
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); i++ {
		c := data[i]
		switch {
		case c == '"':
			start := i
			for i++; i < len(data) && data[i] != '"'; i++ {
				if data[i] == '\\' {
					i++
				}
			}
			out = append(out, data[start:min(i+1, len(data))]...)
		case c == '/' && i+1 < len(data) && data[i+1] == '/':
			for i < len(data) && data[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(data) && data[i+1] == '*':
			end := -1
			for j := i + 2; j+1 < len(data); j++ {
				if data[j] == '*' && data[j+1] == '/' {
					end = j + 1
					break
				}
			}
			if end < 0 {
				// Keep the opener so the result is invalid JSON.
				return append(out, data[i:]...)
			}
			i = end
		case c == '}' || c == ']':
			out = dropTrailingComma(out)
			out = append(out, c)
		default:
			out = append(out, c)
		}
	}
	return out
}

// dropTrailingComma removes a comma that is followed only by whitespace.
func dropTrailingComma(out []byte) []byte {
	for j := len(out) - 1; j >= 0; j-- {
		switch out[j] {
		case ' ', '\t', '\n', '\r':
			continue
		case ',':
			return append(out[:j], out[j+1:]...)
		}
		break
	}
	return out
}
