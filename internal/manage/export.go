package manage

import (
	"strings"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

type fileBundle map[string][]byte

const normalScope = "normal"
const vpngateScope = "vpngate"

func (files fileBundle) json(name string, value any) error {
	data, err := JSONBytes(value)
	if err == nil {
		files[name] = data
	}
	return err
}

// Every delivery path uses these same validated artifacts. The conventional
// filenames are local exports, not a second conversion pipeline.

func appendDefaultExports(files fileBundle, snapshot sub.Snapshot, previews map[string]sub.Preview) error {
	links := map[string][]string{normalScope: {}, vpngateScope: {}}
	for _, scope := range []string{normalScope, vpngateScope} {
		prefix := ""
		if scope == vpngateScope {
			prefix = "vpngate/"
		}
		for name, target := range map[string]string{"sbox.json": "sing-box", "clmi.yaml": "mihomo", "proxies.yaml": "provider", "jhsub.txt": "uri", "jhsub-base64.txt": "base64"} {
			artifact, found := snapshot.Artifacts[scope+"-"+target]
			if found {
				files[prefix+name] = artifact.Data
			} else {
				files[prefix+name] = nil
			}
		}
		if data := files[prefix+"jhsub.txt"]; len(data) > 0 {
			links[scope] = strings.Split(strings.TrimSpace(string(data)), "\n")
		}
		if len(links[scope]) == 0 {
			files[prefix+"share.json"] = nil
			continue
		}
		names := []string{}
		for _, node := range previews[scope].Nodes {
			names = append(names, node.Name)
		}
		share := Object{"links": links[scope], "normal": []string{}, "vpngate": []string{}}
		share[scope] = names
		if err := files.json(prefix+"share.json", share); err != nil {
			return err
		}
	}
	return nil
}
