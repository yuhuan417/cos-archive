package main

import "testing"

func TestApplyRuntimeOverrides(t *testing.T) {
	cfg := Config{
		TargetDir:  "/from-config",
		MountPoint: "/mount-config",
	}

	updated := applyRuntimeOverrides(cfg, "/from-flag", "/mount-flag")
	if updated.TargetDir != "/from-flag" {
		t.Fatalf("unexpected target dir: %s", updated.TargetDir)
	}
	if updated.MountPoint != "/mount-flag" {
		t.Fatalf("unexpected mount point: %s", updated.MountPoint)
	}

	unchanged := applyRuntimeOverrides(cfg, "", "")
	if unchanged.TargetDir != "/from-config" {
		t.Fatalf("unexpected target dir fallback: %s", unchanged.TargetDir)
	}
	if unchanged.MountPoint != "/mount-config" {
		t.Fatalf("unexpected mount point fallback: %s", unchanged.MountPoint)
	}
}
