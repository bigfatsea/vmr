package statsfeed

import "vmr/internal/jsonscan"

// bulkKeys are the audit record members that carry payloads rather than
// facts — request/response bodies, headers, the pre-strip raw copy. They
// are nearly all of a line's bytes and the projection reads none of them.
var bulkKeys = map[string]bool{"body": true, "headers": true, "raw_pre_strip": true}

// prune copies the JSON value at src[i:] into out with every object member
// named in bulkKeys replaced by null, at any depth. Skipping a bulk value
// is a byte scan (jsonscan's quote-aware skip), which is several times
// faster than letting encoding/json tokenize megabytes of message text it
// would only discard.
func prune(src []byte, i int, out []byte) (int, []byte, bool) {
	i = jsonscan.SkipJSONWS(src, i)
	if i >= len(src) {
		return i, out, false
	}
	switch src[i] {
	case '{':
		out = append(out, '{')
		i = jsonscan.SkipJSONWS(src, i+1)
		for first := true; i < len(src) && src[i] != '}'; first = false {
			if !first {
				if src[i] != ',' {
					return i, out, false
				}
				out = append(out, ',')
				i = jsonscan.SkipJSONWS(src, i+1)
			}
			end, ok := jsonscan.SkipJSONString(src, i)
			if !ok {
				return i, out, false
			}
			key := src[i:end]
			out = append(out, key...)
			i = jsonscan.SkipJSONWS(src, end)
			if i >= len(src) || src[i] != ':' {
				return i, out, false
			}
			out = append(out, ':')
			i = jsonscan.SkipJSONWS(src, i+1)
			if bulkKeys[string(key[1:len(key)-1])] {
				if i, ok = jsonscan.SkipJSONValue(src, i); !ok {
					return i, out, false
				}
				out = append(out, "null"...)
			} else if i, out, ok = prune(src, i, out); !ok {
				return i, out, false
			}
			i = jsonscan.SkipJSONWS(src, i)
		}
		if i >= len(src) {
			return i, out, false
		}
		return i + 1, append(out, '}'), true
	case '[':
		out = append(out, '[')
		i = jsonscan.SkipJSONWS(src, i+1)
		for first := true; i < len(src) && src[i] != ']'; first = false {
			if !first {
				if src[i] != ',' {
					return i, out, false
				}
				out = append(out, ',')
				i = jsonscan.SkipJSONWS(src, i+1)
			}
			var ok bool
			if i, out, ok = prune(src, i, out); !ok {
				return i, out, false
			}
			i = jsonscan.SkipJSONWS(src, i)
		}
		if i >= len(src) {
			return i, out, false
		}
		return i + 1, append(out, ']'), true
	default:
		end, ok := jsonscan.SkipJSONValue(src, i)
		if !ok {
			return i, out, false
		}
		return end, append(out, src[i:end]...), true
	}
}
