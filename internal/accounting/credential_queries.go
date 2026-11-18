package accounting

import "context"

func (s *Store) HasCredential(ctx context.Context, scope, id string) (bool, error) {
	var found bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM credentials WHERE id=? AND runtime=?)`, id, scope).Scan(&found)
	return found, err
}

// InboundReferences returns only identifiers. The grant query joins against
// matching bindings, so unrelated credentials and bearer data stay in SQLite.
func (s *Store) InboundReferences(ctx context.Context, scope, id string) (accounts, grants []string, err error) {
	accounts, grants = []string{}, []string{}
	rows, err := s.db.QueryContext(ctx, `WITH bound AS (SELECT account FROM credentials WHERE runtime=? AND inbound=?)
	 SELECT 0,account FROM bound UNION ALL SELECT 1,g.id FROM bound JOIN download_grants g ON g.account=bound.account
	 WHERE g.revoked=0 AND g.scope IN ('',?) ORDER BY 1,2`, scope, id, scope)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var grant bool
		var ref string
		if err = rows.Scan(&grant, &ref); err != nil {
			return nil, nil, err
		}
		if grant {
			grants = append(grants, ref)
		} else {
			accounts = append(accounts, ref)
		}
	}
	return accounts, grants, rows.Err()
}
