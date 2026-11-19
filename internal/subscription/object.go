package subscription

type Object = map[string]any

func fallback(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}
