package assets

import "testing"

func TestFindEmbeddedS3270AssetName_WindowsFallback(t *testing.T) {
	name, err := findEmbeddedS3270AssetName("windows", "amd64", []string{"s3270.exe"})
	if err != nil {
		t.Fatalf("findEmbeddedS3270AssetName returned error: %v", err)
	}
	if name != "s3270.exe" {
		t.Fatalf("name = %q, want %q", name, "s3270.exe")
	}
}

func TestFindEmbeddedS3270AssetName_LinuxPlatformSpecific(t *testing.T) {
	name, err := findEmbeddedS3270AssetName("linux", "amd64", []string{"s3270-linux-amd64", "s3270.exe"})
	if err != nil {
		t.Fatalf("findEmbeddedS3270AssetName returned error: %v", err)
	}
	if name != "s3270-linux-amd64" {
		t.Fatalf("name = %q, want %q", name, "s3270-linux-amd64")
	}
}

func TestFindEmbeddedS3270AssetName_NoPlatformMatch(t *testing.T) {
	_, err := findEmbeddedS3270AssetName("linux", "amd64", []string{"s3270.exe"})
	if err == nil {
		t.Fatal("expected error when no linux embedded binary is available")
	}
}
