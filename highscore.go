package main

import (
	"container/heap"
	"sort"
	"sync"
)

// PlayerScore represents a player's emoji statistics
type PlayerScore struct {
	UserID       string `json:"userId"`
	SpawnedCount int    `json:"spawnedCount"`
	PoppedCount  int    `json:"poppedCount"`
}

// HighScoreEntry represents an entry in the high score list
type HighScoreEntry struct {
	UserID string `json:"userId"`
	Score  int    `json:"score"`
	Rank   int    `json:"rank"`
}

// HighScoreResponse represents the response for the high score endpoint
type HighScoreResponse struct {
	PlayerScore PlayerScore      `json:"playerScore"`
	TopScores   []HighScoreEntry `json:"topScores"`
}

var (
	playerScores    sync.Map
	highScoresMutex sync.RWMutex
	topScores       = make(PriorityQueue, 0)
)

// PriorityQueue implementation
type PriorityQueue []HighScoreEntry

func (pq PriorityQueue) Len() int { return len(pq) }

func (pq PriorityQueue) Less(i, j int) bool {
	return pq[i].Score > pq[j].Score
}

func (pq PriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
}

func (pq *PriorityQueue) Push(x interface{}) {
	*pq = append(*pq, x.(HighScoreEntry))
}

func (pq *PriorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[0 : n-1]
	return item
}

func updatePlayerScore(userID string, action string) {
	score, _ := playerScores.LoadOrStore(userID, &PlayerScore{UserID: userID})
	playerScore := score.(*PlayerScore)

	switch action {
	case "spawn":
		playerScore.SpawnedCount++
	case "pop":
		playerScore.PoppedCount++
	}

	updateHighScores(userID, playerScore.PoppedCount)
}

func updateHighScores(userID string, score int) {
	highScoresMutex.Lock()
	defer highScoresMutex.Unlock()

	// Check if the user is already in the top scores
	for i, entry := range topScores {
		if entry.UserID == userID {
			// Update the score if it's higher
			if score > entry.Score {
				topScores[i].Score = score
				heap.Fix(&topScores, i)
			}
			return
		}
	}

	// If the user is not in the top scores, add them
	if len(topScores) < 10 || score > topScores[len(topScores)-1].Score {
		newEntry := HighScoreEntry{UserID: userID, Score: score}
		heap.Push(&topScores, newEntry)
		if len(topScores) > 10 {
			heap.Pop(&topScores)
		}
	}
}

func getHighScores(userID string) HighScoreResponse {
	highScoresMutex.RLock()
	defer highScoresMutex.RUnlock()

	// Get player's score
	score, ok := playerScores.Load(userID)
	playerScore := PlayerScore{UserID: userID}
	if ok {
		playerScore = *score.(*PlayerScore)
	}

	// Get top 10 scores
	topScoresCopy := make([]HighScoreEntry, len(topScores))
	copy(topScoresCopy, topScores)
	sort.Slice(topScoresCopy, func(i, j int) bool {
		return topScoresCopy[i].Score > topScoresCopy[j].Score
	})

	return HighScoreResponse{
		PlayerScore: playerScore,
		TopScores:   topScoresCopy,
	}
}
