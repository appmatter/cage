package config

import "testing"

func TestNetworkProxyEnabledDefault(t *testing.T) {
	var n Network
	if !n.ProxyEnabled() {
		t.Fatal("default should enable proxy")
	}
	f := false
	n.Proxy.Disabled = &f
	if !n.ProxyEnabled() {
		t.Fatal("proxy.disabled false → proxy on")
	}
	tr := true
	n.Proxy.Disabled = &tr
	if n.ProxyEnabled() {
		t.Fatal("proxy.disabled true → proxy off")
	}
}

func TestNetworkMITMEnabledDefault(t *testing.T) {
	var n Network
	if !n.MITMEnabled() {
		t.Fatal("mitm default on when proxy on")
	}
	f := false
	n.Proxy.MITM = &f
	if n.MITMEnabled() {
		t.Fatal("mitm:false")
	}
	tr := true
	n.Proxy.Disabled = &tr
	n.Proxy.MITM = nil
	if n.MITMEnabled() {
		t.Fatal("mitm off when proxy disabled")
	}
}

func TestMergeNetworkProxy(t *testing.T) {
	tr := true
	base := File{Network: Network{}}
	over := File{Network: Network{Proxy: NetworkProxy{Disabled: &tr, Logging: &tr}}}
	out := mergeFiles(base, over)
	if out.Network.ProxyEnabled() {
		t.Fatalf("disabled: got enabled %#v", out.Network.Proxy)
	}
	if !out.Network.LoggingEnabled() {
		t.Fatalf("logging: got %#v", out.Network.Proxy.Logging)
	}
	f := false
	out2 := mergeFiles(out, File{Network: Network{Proxy: NetworkProxy{Logging: &f}}})
	if out2.Network.LoggingEnabled() {
		t.Fatalf("expected logging off, got %#v", out2.Network.Proxy)
	}
	if out2.Network.ProxyEnabled() {
		t.Fatalf("disabled should remain, got %#v", out2.Network.Proxy)
	}
	out3 := mergeFiles(out2, File{Network: Network{Plugins: NetworkPlugins{Egress: &Egress{DenyResponse: &DenyResponse{HTTP: true, Message: "nope"}}}}})
	if !out3.Network.Plugins.Egress.DenyHTTPResponse() || out3.Network.Plugins.Egress.DenyHTTPMessage() != "nope" {
		t.Fatalf("deny_response: %#v", out3.Network.Plugins.Egress.DenyResponse)
	}
	out4 := mergeFiles(out3, File{Network: Network{Proxy: NetworkProxy{MITM: &f}}})
	if out4.Network.Proxy.MITM == nil || *out4.Network.Proxy.MITM {
		t.Fatalf("mitm merge: %#v", out4.Network.Proxy.MITM)
	}
}
