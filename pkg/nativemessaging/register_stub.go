//go:build !windows

package nativemessaging

import "errors"

func RegisterHost(hostBinaryPath, extensionID string) (string, error) {
	return "", errors.New("native messaging registry registration is only supported on Windows")
}

func CheckRegistration() RegistrationStatus {
	manifestPath, _ := DefaultManifestPath()
	return RegistrationStatus{ManifestPath: manifestPath}
}
