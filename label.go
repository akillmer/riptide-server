package main

import (
	"encoding/json"
	"errors"

	db "github.com/akillmer/riptide/database"
	"github.com/teris-io/shortid"
)

// Label that can be assigned to a torrent
type Label struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Color  string `json:"color"`
	MoveTo string `json:"moveToPath"`
	// `moveTo` gets dropped by react, guessing it's reserved?
}

// Errors
var (
	ErrLabelNotFound = errors.New("label not found")
)

// GetLabel by its ID
func GetLabel(id string) (*Label, error) {
	lbl := &Label{}
	buf, err := db.Get(db.BucketLabels, id)
	if err != nil {
		return nil, err
	}
	if buf == nil {
		return nil, ErrLabelNotFound
	}
	if err := json.Unmarshal(buf, lbl); err != nil {
		return nil, err
	}
	return lbl, nil
}

// LabelFromPayload creates a new label from a socket message payload
func LabelFromPayload(data map[string]interface{}) (*Label, error) {
	label := &Label{}

	if id, ok := data["id"].(string); ok {
		label.ID = id
	}

	if name, ok := data["name"].(string); ok && name != "" {
		label.Name = name
	} else {
		return nil, errors.New("label is missing name")
	}

	if color, ok := data["color"].(string); ok {
		label.Color = color
	}

	if moveTo, ok := data["moveToPath"].(string); ok {
		label.MoveTo = moveTo
	}

	return label, nil
}

// Save this Label with the database. If it's a new label then a new short id is assigned.
func (lbl *Label) Save() error {
	if lbl.ID == "" {
		id, err := shortid.Generate()
		if err != nil {
			return err
		}
		lbl.ID = id
	}
	return db.Put(db.BucketLabels, lbl.ID, lbl)
}

// DeleteLabel from the database.
func DeleteLabel(id string) error {
	return db.Delete(db.BucketLabels, id)
}
