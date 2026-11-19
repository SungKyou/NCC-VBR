// 출처: ncc-xen stdcrypto/secret/keyring.go (커밋 043c6d5, 2026-11-17 복사)
// 바뀐 부분: docs/agent/08-copied-code-register.md CP-10

package secret

type keyEntry struct {
	algorithm string
	material  []byte
}

type keyring struct {
	activeKeyID string
	entries     map[string]keyEntry
}

func (k *keyring) active() (string, keyEntry) {
	entry := k.entries[k.activeKeyID]
	return k.activeKeyID, entry
}

func (k *keyring) find(id string) (keyEntry, bool) {
	entry, ok := k.entries[id]
	return entry, ok
}
