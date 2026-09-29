package ext

// ConfigurationLoadsModule reports an exact module loading directive, not its filename.
func ConfigurationLoadsModule(data []byte, module string) bool {
	module = canonicalModule(module)
	for _, directive := range loadingDirectives(data) {
		if directive.module == module {
			return true
		}
	}
	return false
}
