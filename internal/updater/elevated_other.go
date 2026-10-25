//go:build !linux

package updater

import "errors"

func RunElevatedHelper(string, string) error {
	return errors.New("privileged Linux updater helper is unavailable on this platform")
}

func runElevatedUpdate(string, string, Plan) error {
	return errors.New("privileged Linux updater is unavailable on this platform")
}
