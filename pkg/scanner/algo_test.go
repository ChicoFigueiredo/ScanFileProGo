package scanner

import "testing"

func TestNormalizeHashAlgorithm(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", HashXXHash},
		{"xxhash", HashXXHash},
		{"XXHASH", HashXXHash},
		{"xxh64", HashXXHash},
		{"blake3", HashBlake3},
		{"BLAKE3", HashBlake3},
		{"md5", HashMD5},
		{"sha256", HashSHA256},
		{"sha-256", HashSHA256},
		{"inexistente", HashXXHash},
	}
	for _, c := range cases {
		if got := NormalizeHashAlgorithm(c.in); got != c.want {
			t.Errorf("NormalizeHashAlgorithm(%q) = %q, quer %q", c.in, got, c.want)
		}
	}
}

func TestHashPrefix(t *testing.T) {
	cases := map[string]string{
		HashXXHash: "xxh64:",
		HashBlake3: "blake3:",
		HashMD5:    "md5:",
		HashSHA256: "sha256:",
	}
	for algo, want := range cases {
		if got := HashPrefix(algo); got != want {
			t.Errorf("HashPrefix(%q) = %q, quer %q", algo, got, want)
		}
	}
}

func TestHashMatchesAlgorithm(t *testing.T) {
	cases := []struct {
		hash string
		algo string
		want bool
	}{
		{"xxh64:0102030405060708", "xxhash", true},
		{"xxh64:0102030405060708", "blake3", false},
		{"blake3:" + "ab", "blake3", true},
		{"md5:ab", "md5", true},
		{"sha256:ab", "sha256", true},
		{"sha256:ab", "xxhash", false},
		{"", "xxhash", false},
		{"semprefixo", "xxhash", false},
		{"XXH64:0102030405060708", "xxhash", true}, // prefixo case-insensitive
	}
	for _, c := range cases {
		if got := HashMatchesAlgorithm(c.hash, c.algo); got != c.want {
			t.Errorf("HashMatchesAlgorithm(%q,%q) = %v, quer %v", c.hash, c.algo, got, c.want)
		}
	}
}

func TestSupportedHashAlgorithms(t *testing.T) {
	got := SupportedHashAlgorithms()
	want := []string{HashXXHash, HashBlake3, HashMD5, HashSHA256}
	if len(got) != len(want) {
		t.Fatalf("esperava %d algoritmos, obtive %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("posição %d: %q, quer %q", i, got[i], want[i])
		}
	}
}
