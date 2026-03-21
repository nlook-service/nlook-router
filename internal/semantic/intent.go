package semantic

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"os"
	"path/filepath"
	"sync"
)

// IntentStore manages intent categories with their embedding vectors.
type IntentStore struct {
	mu       sync.RWMutex
	intents  []IntentCategory
	embedder Embedder
	filePath string
}

// NewIntentStore creates an intent store with persistence path.
func NewIntentStore(embedder Embedder, dataDir string) *IntentStore {
	return &IntentStore{
		intents:  DefaultIntents(),
		embedder: embedder,
		filePath: filepath.Join(dataDir, "intent_vectors.json"),
	}
}

// DefaultIntents returns the initial intent categories.
func DefaultIntents() []IntentCategory {
	return []IntentCategory{
		{Name: "greeting", Complexity: "simple", MinTier: 1, PreferredTier: 1,
			Texts: []string{"안녕하세요", "hello", "잘지내?", "좋은 아침", "hi", "반가워"}},
		{Name: "task_query", Complexity: "simple", MinTier: 1, PreferredTier: 1,
			Texts: []string{"할일 보여줘", "오늘 일정", "task list", "할일 목록", "진행중인 작업", "오늘 할 일"}},
		{Name: "task_create", Complexity: "simple", MinTier: 1, PreferredTier: 1,
			Texts: []string{"할일 추가해", "등록해줘", "할일 만들어", "일정 추가", "새 할일"}},
		{Name: "doc_query", Complexity: "simple", MinTier: 1, PreferredTier: 1,
			Texts: []string{"문서 보여줘", "메모 목록", "문서 검색", "노트 찾아", "글 목록"}},
		{Name: "code_analysis", Complexity: "complex", MinTier: 2, PreferredTier: 2,
			Texts: []string{"코드 분석", "버그 찾아", "코딩 분석", "리팩토링해줘", "코드 리뷰", "코드 설명해줘"}},
		{Name: "writing", Complexity: "complex", MinTier: 2, PreferredTier: 2,
			Texts: []string{"글 써줘", "블로그 작성", "번역해줘", "요약해줘", "이메일 작성", "문장 다듬어줘"}},
		{Name: "web_search", Complexity: "complex", MinTier: 2, PreferredTier: 2,
			Texts: []string{"날씨 어때", "검색해줘", "뉴스 알려줘", "몇시야", "실시간 정보", "최신 소식"}},
		{Name: "deep_analysis", Complexity: "reasoning", MinTier: 3, PreferredTier: 3,
			Texts: []string{"원인 분석", "비교 분석해줘", "전략 수립", "장단점 비교", "왜 이런 결과가", "근본 원인 파악"}},
		{Name: "system_question", Complexity: "complex", MinTier: 2, PreferredTier: 2,
			Texts: []string{"기능이 안돼", "왜 안되지", "에러나요", "작동 안해", "기능 있어?", "안돼요"}},
	}
}

// Initialize embeds all representative texts and computes centroids.
// Loads cached vectors from file if available; only embeds missing ones.
func (s *IntentStore) Initialize(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Try loading cached vectors
	if s.loadFromFile() {
		// Recompute centroids from loaded vectors
		for i := range s.intents {
			s.intents[i].Centroid = computeCentroid(s.intents[i].Vectors)
		}
		log.Printf("semantic/intent: loaded %d categories from cache", len(s.intents))
		return nil
	}

	// Embed all texts for each category
	for i := range s.intents {
		cat := &s.intents[i]
		vecs, err := s.embedder.EmbedBatch(ctx, cat.Texts)
		if err != nil {
			return err
		}
		cat.Vectors = vecs
		cat.Centroid = computeCentroid(vecs)
	}

	s.saveToFile()
	log.Printf("semantic/intent: initialized %d categories", len(s.intents))
	return nil
}

// Match finds the best matching intent for a query embedding.
func (s *IntentStore) Match(queryVec []float32) (name string, score float64) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	bestScore := float64(-1)
	bestName := "general"

	for _, cat := range s.intents {
		if len(cat.Centroid) == 0 {
			continue
		}
		sim := float64(cosineSimilarity(queryVec, cat.Centroid))
		if sim > bestScore {
			bestScore = sim
			bestName = cat.Name
		}
	}

	return bestName, bestScore
}

// Get returns an intent category by name.
func (s *IntentStore) Get(name string) *IntentCategory {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.intents {
		if s.intents[i].Name == name {
			return &s.intents[i]
		}
	}
	return nil
}

// GetComplexity returns the complexity level for an intent.
func (s *IntentStore) GetComplexity(name string) string {
	cat := s.Get(name)
	if cat == nil {
		return "complex" // safe default
	}
	return cat.Complexity
}

// AddVector adds a feedback-learned vector to an intent category.
func (s *IntentStore) AddVector(intent string, vec []float32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.intents {
		if s.intents[i].Name == intent {
			s.intents[i].Vectors = append(s.intents[i].Vectors, vec)
			s.intents[i].Centroid = computeCentroid(s.intents[i].Vectors)
			return
		}
	}
}

// RaiseMinTier increases the minimum tier for a category.
func (s *IntentStore) RaiseMinTier(intent string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.intents {
		if s.intents[i].Name == intent && s.intents[i].MinTier < 4 {
			s.intents[i].MinTier++
			log.Printf("semantic/intent: raised min_tier for %s to %d", intent, s.intents[i].MinTier)
			return
		}
	}
}

// LowerMinTier decreases the minimum tier for a category.
func (s *IntentStore) LowerMinTier(intent string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.intents {
		if s.intents[i].Name == intent && s.intents[i].MinTier > 1 {
			s.intents[i].MinTier--
			log.Printf("semantic/intent: lowered min_tier for %s to %d", intent, s.intents[i].MinTier)
			return
		}
	}
}

// Save persists current vectors to disk.
func (s *IntentStore) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	s.saveToFile()
	return nil
}

func (s *IntentStore) loadFromFile() bool {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return false
	}
	var cached []IntentCategory
	if err := json.Unmarshal(data, &cached); err != nil {
		return false
	}
	// Merge cached vectors into defaults (preserving new categories)
	cacheMap := make(map[string]*IntentCategory, len(cached))
	for i := range cached {
		cacheMap[cached[i].Name] = &cached[i]
	}
	for i := range s.intents {
		if c, ok := cacheMap[s.intents[i].Name]; ok && len(c.Vectors) > 0 {
			s.intents[i].Vectors = c.Vectors
			s.intents[i].MinTier = c.MinTier
			s.intents[i].PreferredTier = c.PreferredTier
		} else {
			return false // missing vectors, need full re-embed
		}
	}
	return true
}

func (s *IntentStore) saveToFile() {
	data, err := json.Marshal(s.intents)
	if err != nil {
		log.Printf("semantic/intent: save error: %v", err)
		return
	}
	os.MkdirAll(filepath.Dir(s.filePath), 0755)
	if err := os.WriteFile(s.filePath, data, 0644); err != nil {
		log.Printf("semantic/intent: write error: %v", err)
	}
}

func computeCentroid(vecs [][]float32) []float32 {
	if len(vecs) == 0 {
		return nil
	}
	dim := len(vecs[0])
	centroid := make([]float32, dim)
	for _, v := range vecs {
		for j := range v {
			centroid[j] += v[j]
		}
	}
	n := float32(len(vecs))
	for j := range centroid {
		centroid[j] /= n
	}
	return centroid
}

func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return float32(dot / denom)
}
