package conversation

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
)

// loadSessions reads the session index. sqlite3 renders each row as a JSON
// object; an empty result prints nothing, which is no sessions rather than an
// error. A hidden session is one the operator archived and is left out.
func loadSessions(db string) ([]sessionRow, error) {
	const query = `SELECT id, model, working_directory, title, last_activity_at, ` +
		`main_chain_id FROM sessions WHERE hidden = 0`
	rows, err := run(db, query)
	if err != nil {
		return nil, err
	}
	var raw []struct {
		ID           string `json:"id"`
		Model        string `json:"model"`
		Directory    string `json:"working_directory"`
		Title        string `json:"title"`
		LastActivity int64  `json:"last_activity_at"`
		MainChain    *int64 `json:"main_chain_id"`
	}
	if err := decode(rows, &raw); err != nil {
		return nil, err
	}
	out := make([]sessionRow, 0, len(raw))
	for _, one := range raw {
		row := sessionRow{
			ID: one.ID, Model: one.Model, Directory: one.Directory,
			Title: one.Title, LastActivity: one.LastActivity,
		}
		if one.MainChain != nil {
			row.MainChain, row.HasMainChain = *one.MainChain, true
		}
		out = append(out, row)
	}
	return out, nil
}

// loadNodes reads one session's message forest. The chat_message column is a
// JSON document stored as text, so sqlite3 hands it back as a quoted string that
// is decoded once into the string and again into the message it holds.
func loadNodes(db, session string) ([]nodeRow, error) {
	query := `SELECT node_id, parent_node_id, chat_message FROM message_nodes ` +
		`WHERE session_id = '` + escape(session) + `' ORDER BY node_id`
	rows, err := run(db, query)
	if err != nil {
		return nil, err
	}
	var raw []struct {
		NodeID  int64  `json:"node_id"`
		Parent  *int64 `json:"parent_node_id"`
		Message string `json:"chat_message"`
	}
	if err := decode(rows, &raw); err != nil {
		return nil, err
	}
	out := make([]nodeRow, 0, len(raw))
	for _, one := range raw {
		node := nodeRow{NodeID: one.NodeID}
		if one.Parent != nil {
			node.Parent, node.HasPar = *one.Parent, true
		}
		// A node the reader cannot decode is skipped rather than failing the
		// session: the rest of the run is still the truth.
		if json.Unmarshal([]byte(one.Message), &node.Message) != nil {
			continue
		}
		out = append(out, node)
	}
	return out, nil
}

// run executes one read-only query and returns its JSON rows. The database is
// opened read-only so a poll never contends with the CLI writing a live turn.
func run(db, query string) ([]byte, error) {
	if _, err := os.Stat(db); err != nil {
		return nil, err
	}
	cmd := exec.Command("sqlite3", "-readonly", "-json", db, query)
	return cmd.Output()
}

// decode reads sqlite3's JSON array. An empty result is an empty slice.
func decode(rows []byte, into any) error {
	if len(strings.TrimSpace(string(rows))) == 0 {
		return nil
	}
	return json.Unmarshal(rows, into)
}

// escape doubles a single quote so a session id is a safe string literal. Devin
// ids are hyphenated words, so this only guards against a malformed argument.
func escape(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}
