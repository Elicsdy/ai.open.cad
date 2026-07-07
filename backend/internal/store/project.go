package store

import "time"

type Project struct {
	ID        string    `json:"id"`
	OwnerID   string    `json:"ownerId,omitempty"`
	Title     string    `json:"title"`
	Prompt    string    `json:"prompt"`
	Code      string    `json:"code"`
	Language  string    `json:"language"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ProjectInput struct {
	OwnerID  string `json:"ownerId,omitempty"`
	Title    string `json:"title"`
	Prompt   string `json:"prompt"`
	Code     string `json:"code"`
	Language string `json:"language"`
}
