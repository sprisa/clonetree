//go:build !darwin

package main

import "context"

func prepareClonePlan(ctx context.Context, root string) (*clonePlan, error) {
	return nil, nil
}

func cloneDirectoryNative(source, destination string) (bool, error) {
	return false, nil
}
