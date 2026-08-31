package config

import "testing"

func TestValidateRuntimePrioritiesRequired(t *testing.T) {
	f := File{
		Runtime: Runtime{Plugins: map[string]RuntimePlugin{
			"tart":   {Image: "a"},
			"docker": {Image: "b"},
		}},
	}
	if err := ValidateFile(f); err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateTerminatePrioritiesRequired(t *testing.T) {
	f := File{
		Network: Network{Plugins: NetworkPlugins{
			HTTPProxy: &ProtocolProxies{Endpoints: map[string]Proxy{"a": {URL: "https://a"}}},
			PostgresProxy: &ProtocolProxies{
				Priority:  intPtr(1),
				Endpoints: map[string]Proxy{"b": {Listen: 5432}},
			},
		}},
	}
	if err := ValidateFile(f); err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateTerminatePrioritiesOK(t *testing.T) {
	f := File{
		Network: Network{Plugins: NetworkPlugins{
			HTTPProxy: &ProtocolProxies{
				Priority:  intPtr(1),
				Endpoints: map[string]Proxy{"a": {URL: "https://a"}},
			},
			PostgresProxy: &ProtocolProxies{
				Priority:  intPtr(2),
				Endpoints: map[string]Proxy{"b": {Listen: 5432}},
			},
		}},
	}
	if err := ValidateFile(f); err != nil {
		t.Fatal(err)
	}
}

func TestValidateTerminateDuplicatePriority(t *testing.T) {
	f := File{
		Network: Network{Plugins: NetworkPlugins{
			HTTPProxy: &ProtocolProxies{
				Priority:  intPtr(1),
				Endpoints: map[string]Proxy{"a": {URL: "https://a"}},
			},
			PostgresProxy: &ProtocolProxies{
				Priority:  intPtr(1),
				Endpoints: map[string]Proxy{"b": {Listen: 5432}},
			},
		}},
	}
	if err := ValidateFile(f); err == nil {
		t.Fatal("expected duplicate priority error")
	}
}

func TestValidateSingleTerminateNoPriority(t *testing.T) {
	f := File{
		Network: Network{Plugins: NetworkPlugins{
			HTTPProxy: &ProtocolProxies{Endpoints: map[string]Proxy{"a": {URL: "https://a"}}},
		}},
	}
	if err := ValidateFile(f); err != nil {
		t.Fatal(err)
	}
}

func TestValidateSecretsAliasUnique(t *testing.T) {
	f := File{
		Secrets: map[string]map[string]SecretStore{
			"onepassword": {"shared": {Vars: map[string]string{"A": "op://a"}}},
			"keychain":    {"shared": {Vars: map[string]string{"B": "B"}}},
		},
	}
	if err := ValidateFile(f); err == nil {
		t.Fatal("expected duplicate alias error")
	}
}

func TestValidateSecretsDAG(t *testing.T) {
	f := File{
		Secrets: map[string]map[string]SecretStore{
			"onepassword": {
				"personal-op": {Vars: map[string]string{"AWS_KEY": "op://x"}},
			},
			"aws_sm": {
				"dev-sm": {
					Uses: []string{"personal-op"},
					Vars: map[string]string{"DB": "arn:x"},
				},
			},
		},
	}
	if err := ValidateFile(f); err != nil {
		t.Fatal(err)
	}
}

func TestValidateSecretsCycle(t *testing.T) {
	f := File{
		Secrets: map[string]map[string]SecretStore{
			"onepassword": {
				"a": {Uses: []string{"b"}, Vars: map[string]string{"X": "1"}},
				"b": {Uses: []string{"a"}, Vars: map[string]string{"Y": "2"}},
			},
		},
	}
	if err := ValidateFile(f); err == nil {
		t.Fatal("expected cycle error")
	}
}

func TestValidateSecretsTemplateDep(t *testing.T) {
	f := File{
		Secrets: map[string]map[string]SecretStore{
			"onepassword": {
				"op": {Vars: map[string]string{"K": "op://k"}},
			},
			"file": {
				"f": {Vars: map[string]string{"X": "{{ secrets.op.K }}"}},
			},
		},
	}
	if err := ValidateFile(f); err != nil {
		t.Fatal(err)
	}
}

func intPtr(n int) *int { return &n }
