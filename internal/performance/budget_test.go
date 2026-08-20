package performance_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocalDashboardAssetBudget(t *testing.T) {
	budgets := map[string]int64{
		filepath.Join("..", "web", "static", "app.js"):    10 << 10,
		filepath.Join("..", "web", "static", "style.css"): 50 << 10,
	}
	for path, budget := range budgets {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if info.Size() > budget {
			t.Fatalf("%s is %d bytes, budget is %d", path, info.Size(), budget)
		}
	}
}
