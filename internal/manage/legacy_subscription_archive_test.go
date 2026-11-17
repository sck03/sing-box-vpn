package manage

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"time"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

type subscriptionArchiveEntry struct {
	ID    string           `json:"id"`
	Kind  string           `json:"kind"`
	Time  time.Time        `json:"time"`
	Data  json.RawMessage  `json:"data"`
	Cache *sub.SourceCache `json:"cache,omitempty"`
}

func archiveObject[T any](items []T, id string, getID func(T) string) ([]T, json.RawMessage, error) {
	for i, item := range items {
		if getID(item) == id {
			data, err := json.Marshal(item)
			return append(items[:i:i], items[i+1:]...), data, err
		}
	}
	return items, nil, errors.New("object not found")
}

func restoreObject[T any](items []T, data []byte, getID func(T) string) ([]T, error) {
	var item T
	if err := strictSubscriptionJSON(data, &item); err != nil {
		return items, err
	}
	for _, existing := range items {
		if getID(existing) == getID(item) {
			return items, errors.New("object ID already exists")
		}
	}
	return append(items, item), nil
}

func subscriptionArchive(root, core, operation, kind, id string, c sub.Config, r sub.Runtime, local map[string][]sub.Node, ctx context.Context, out io.Writer) error {
	const archivePath = "subscriptions/trash.json"
	entries := []subscriptionArchiveEntry{}
	if _, err := ReadJSON(filepath.Join(root, archivePath), &entries); err != nil {
		return err
	}
	if operation == "trash" {
		return jsonEncoder(out).Encode(entries)
	}
	var data json.RawMessage
	var sourceCache *sub.SourceCache
	var err error
	if operation == "archive" {
		switch kind {
		case "source":
			c.Sources, data, err = archiveObject(c.Sources, id, func(v sub.Source) string { return v.ID })
			if cache, exists := r.Sources[id]; exists {
				sourceCache = &cache
			}
		case "collection":
			c.Collections, data, err = archiveObject(c.Collections, id, func(v sub.Collection) string { return v.ID })
		case "template":
			c.Templates, data, err = archiveObject(c.Templates, id, func(v sub.Template) string { return v.ID })
		case "output":
			c.Outputs, data, err = archiveObject(c.Outputs, id, func(v sub.Output) string { return v.ID })
		case "rules":
			c.RuleSets, data, err = archiveObject(c.RuleSets, id, func(v sub.RuleSet) string { return v.ID })
		default:
			return errors.New("archive supports source, collection, template, output and rules")
		}
		if err != nil {
			return err
		}
		entries = append(entries, subscriptionArchiveEntry{ID: randomSecret()[:16], Kind: kind, Time: time.Now(), Data: data, Cache: sourceCache})
	} else {
		index := -1
		for i, entry := range entries {
			if entry.ID == id {
				index = i
				kind, data = entry.Kind, entry.Data
				sourceCache = entry.Cache
				break
			}
		}
		if index < 0 {
			return errors.New("archive entry not found")
		}
		switch kind {
		case "source":
			c.Sources, err = restoreObject(c.Sources, data, func(v sub.Source) string { return v.ID })
			if err == nil && sourceCache != nil {
				if r.Sources == nil {
					r.Sources = map[string]sub.SourceCache{}
				}
				r.Sources[c.Sources[len(c.Sources)-1].ID] = *sourceCache
			}
		case "collection":
			c.Collections, err = restoreObject(c.Collections, data, func(v sub.Collection) string { return v.ID })
		case "template":
			c.Templates, err = restoreObject(c.Templates, data, func(v sub.Template) string { return v.ID })
		case "output":
			c.Outputs, err = restoreObject(c.Outputs, data, func(v sub.Output) string { return v.ID })
		case "rules":
			c.RuleSets, err = restoreObject(c.RuleSets, data, func(v sub.RuleSet) string { return v.ID })
		default:
			return errors.New("invalid archive type")
		}
		if err != nil {
			return err
		}
		entries = append(entries[:index:index], entries[index+1:]...)
	}
	if err := c.Validate(); err != nil {
		return err
	}
	c.Revision++
	if kind == "source" && operation == "archive" {
		delete(r.Sources, id)
	}
	files, _, _, err := buildSubscriptions(ctx, root, core, c, r, local)
	if err != nil {
		return err
	}
	// Keep no more than 32 records, 30 days or 2 MiB of archived objects.
	kept := []subscriptionArchiveEntry{}
	total := 0
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		encoded, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if len(kept) == 32 || total+len(encoded) > 2<<20 || time.Since(e.Time) > 30*24*time.Hour {
			continue
		}
		kept = append([]subscriptionArchiveEntry{e}, kept...)
		total += len(encoded)
	}
	if err := files.json(archivePath, kept); err != nil {
		return err
	}
	return publish(root, files)
}
