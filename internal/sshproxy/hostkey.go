package sshproxy

import (
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// knownHostsPath returns the path to the nlook known_hosts file.
func knownHostsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	dir := filepath.Join(home, ".nlook")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create ~/.nlook: %w", err)
	}
	return filepath.Join(dir, "known_hosts"), nil
}

// newHostKeyCallback returns an ssh.HostKeyCallback implementing TOFU
// (Trust On First Use). On first connection the key is saved to
// ~/.nlook/known_hosts; on subsequent connections it is verified.
func newHostKeyCallback() (ssh.HostKeyCallback, error) {
	path, err := knownHostsPath()
	if err != nil {
		return nil, err
	}

	// Ensure the file exists so knownhosts.New can open it.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("open known_hosts: %w", err)
	}
	f.Close()

	checkKnown, err := knownhosts.New(path)
	if err != nil {
		return nil, fmt.Errorf("load known_hosts: %w", err)
	}

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := checkKnown(hostname, remote, key)
		if err == nil {
			// Key matches — verified.
			return nil
		}

		// knownhosts returns *knownhosts.KeyError when the host is unknown
		// (Wants is empty) or when the key has changed (Wants is non-empty).
		var keyErr *knownhosts.KeyError
		if !isKeyError(err, &keyErr) {
			return err
		}

		if len(keyErr.Want) > 0 {
			// Key mismatch — possible MITM, refuse connection.
			return fmt.Errorf(
				"ssh: host key mismatch for %s — expected %s but got %s. "+
					"If the server key genuinely changed, remove its entry from ~/.nlook/known_hosts",
				hostname,
				knownhosts.Normalize(keyErr.Want[0].Filename),
				ssh.FingerprintSHA256(key),
			)
		}

		// Host not yet known — save the key (TOFU).
		if err := appendKnownHost(path, hostname, remote, key); err != nil {
			return fmt.Errorf("save host key: %w", err)
		}
		log.Printf("ssh: trusted new host key for %s (%s)", hostname, ssh.FingerprintSHA256(key))
		return nil
	}, nil
}

// isKeyError asserts err to *knownhosts.KeyError via errors.As semantics.
func isKeyError(err error, out **knownhosts.KeyError) bool {
	ke, ok := err.(*knownhosts.KeyError)
	if ok {
		*out = ke
	}
	return ok
}

// appendKnownHost appends a single host entry to the known_hosts file.
func appendKnownHost(path, hostname string, remote net.Addr, key ssh.PublicKey) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return fmt.Errorf("open known_hosts for writing: %w", err)
	}
	defer f.Close()

	line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key)
	_, err = fmt.Fprintln(f, line)
	return err
}
