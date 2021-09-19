package ci

import (
	"math"
	"os"
	"path/filepath"
	"strings"
)

func getWebIntegrationFileNames() []string {
	var fileNames []string

	err := filepath.Walk("client/web/src/integration", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if strings.HasSuffix(path, ".test.ts") {
			fileNames = append(fileNames, path)
		}

		return nil
	})

	if err != nil {
		panic(err)
	}

	return fileNames
}

func chunkItems(items []string, size int) [][]string {
	lenItems := len(items)
	lenChunks := lenItems/size + 1
	chunks := make([][]string, lenChunks)

	for i := range chunks {
		start := i * size
		end := min(start+size, lenItems)
		chunks[i] = items[start:end]
	}

	return chunks
}

func min(x int, y int) int {
	return int(math.Min(float64(x), float64(y)))
}

func getChunkedWebIntegrationFileNames(chunkSize int) []string {
	testFiles := getWebIntegrationFileNames()
	chunkedTestFiles := chunkItems(testFiles, chunkSize)
	chunkedTestFileStrings := make([]string, len(chunkedTestFiles))

	for i, v := range chunkedTestFiles {
		chunkedTestFileStrings[i] = strings.Join(v, " ")
	}

	return chunkedTestFileStrings
}
