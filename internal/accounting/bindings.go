package accounting

import (
	"context"
	"encoding/json"
)

// Binding is the public identity of an inbound credential. Share readiness
// never needs the password, UUID or other secret material.
type Binding struct{ Runtime, Inbound, Protocol string }

func (s *Store) AccountBindings(ctx context.Context, ids []string) (map[string][]Binding, error) {
	result := make(map[string][]Binding)
	if len(ids) == 0 {
		return result, nil
	}
	data, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT account,runtime,inbound,protocol FROM credentials WHERE account IN (SELECT value FROM json_each(?))`, string(data))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var b Binding
		if err := rows.Scan(&id, &b.Runtime, &b.Inbound, &b.Protocol); err != nil {
			return nil, err
		}
		result[id] = append(result[id], b)
	}
	return result, rows.Err()
}
