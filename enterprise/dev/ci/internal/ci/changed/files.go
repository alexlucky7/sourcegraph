package changed

func contains(s []string, str string) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}
	return false
}

// Changes in the root directory files should trigger client tests.
var clientRootFiles = []string{
	"package.json",
	"yarn.lock",
	"jest.config.base.js",
	"jest.config.js",
	"postcss.config.js",
	"tsconfig.all.json",
	"tsconfig.json",
	"babel.config.js",
	".percy.yml",
	".eslintrc.js",
	"gulpfile.js",
}
