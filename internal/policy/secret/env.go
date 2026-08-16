// Package secret implements constrained references to namespace-local
// Kubernetes Secrets. It builds SecretKeySelectors without reading Secret
// objects or their values through the TestTender service account.
package secret

import (
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	// EnvLabelPrefix identifies Docker labels that request a Secret-backed
	// environment variable. The suffix is the environment variable name and
	// the value has the form <secret-name>:<secret-key>.
	EnvLabelPrefix = "testtender.io/secret-env."
	// FileLabelPrefix identifies Docker labels that mount one Secret key as a
	// read-only file below /run/secrets. The suffix becomes the filename.
	FileLabelPrefix = "testtender.io/secret-file."
	// VolumeName is reserved for the projected volume built by this policy.
	VolumeName = "testtender-secrets"
	// MountPath matches the conventional Docker secrets directory.
	MountPath = "/run/secrets"

	maxEnvReferences = 32
)

// Policy authorizes exact Secret names and translates labels into Kubernetes
// EnvVar references. The policy is immutable after construction.
type Policy struct {
	allowed map[string]struct{}
}

// Files returns a projected Secret volume and read-only mount for secret-file
// labels. Secret values remain resolved by the kubelet and are never returned
// to TestTender.
func (p *Policy) Files(labels map[string]string) ([]corev1.Volume, []corev1.VolumeMount, error) {
	labelKeys := make([]string, 0)
	for label := range labels {
		if strings.HasPrefix(label, FileLabelPrefix) {
			labelKeys = append(labelKeys, label)
		}
	}
	if len(labelKeys) > maxEnvReferences {
		return nil, nil, fmt.Errorf("too many Secret file references: %d exceeds maximum %d", len(labelKeys), maxEnvReferences)
	}
	if len(labelKeys) == 0 {
		return nil, nil, nil
	}
	sort.Strings(labelKeys)

	filesBySecret := make(map[string][]corev1.KeyToPath)
	seenFiles := make(map[string]struct{}, len(labelKeys))
	for _, label := range labelKeys {
		fileName := strings.TrimPrefix(label, FileLabelPrefix)
		if fileName == "." || fileName == ".." {
			return nil, nil, fmt.Errorf("invalid Secret filename %q", fileName)
		}
		if problems := validation.IsConfigMapKey(fileName); len(problems) > 0 {
			return nil, nil, fmt.Errorf("invalid Secret filename %q: %s", fileName, strings.Join(problems, ", "))
		}
		if _, exists := seenFiles[fileName]; exists {
			return nil, nil, fmt.Errorf("duplicate Secret filename %q", fileName)
		}
		seenFiles[fileName] = struct{}{}

		secretName, secretKey, err := p.parseReference(fileName, labels[label])
		if err != nil {
			return nil, nil, err
		}
		filesBySecret[secretName] = append(filesBySecret[secretName], corev1.KeyToPath{
			Key:  secretKey,
			Path: fileName,
		})
	}

	secretNames := make([]string, 0, len(filesBySecret))
	for secretName := range filesBySecret {
		secretNames = append(secretNames, secretName)
	}
	sort.Strings(secretNames)
	sources := make([]corev1.VolumeProjection, 0, len(secretNames))
	for _, secretName := range secretNames {
		sources = append(sources, corev1.VolumeProjection{Secret: &corev1.SecretProjection{
			LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
			Items:                filesBySecret[secretName],
		}})
	}
	mode := int32(0440)
	return []corev1.Volume{{
			Name: VolumeName,
			VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{
				Sources:     sources,
				DefaultMode: &mode,
			}},
		}}, []corev1.VolumeMount{{
			Name:      VolumeName,
			MountPath: MountPath,
			ReadOnly:  true,
		}}, nil
}

// Validate checks every supported Secret reference without producing a Pod
// specification.
func (p *Policy) Validate(labels map[string]string, existing []corev1.EnvVar) error {
	if _, err := p.EnvVars(labels, existing); err != nil {
		return err
	}
	_, _, err := p.Files(labels)
	return err
}

// DeniedError reports a namespace-local Secret that is not in the configured
// exact allowlist.
type DeniedError struct {
	SecretName string
}

func (e *DeniedError) Error() string {
	return fmt.Sprintf("Secret %q is not allowed for TestTender workloads", e.SecretName)
}

// New validates an exact Secret-name allowlist. An empty allowlist disables
// Secret references and causes any secret-env label to fail closed.
func New(allowed []string) (*Policy, error) {
	policy := &Policy{allowed: make(map[string]struct{}, len(allowed))}
	for _, raw := range allowed {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if problems := validation.IsDNS1123Subdomain(name); len(problems) > 0 {
			return nil, fmt.Errorf("invalid allowed Secret name %q: %s", name, strings.Join(problems, ", "))
		}
		policy.allowed[name] = struct{}{}
	}
	return policy, nil
}

// EnvVars returns deterministic SecretKeySelector environment variables for
// the supplied container labels. Existing ordinary environment variable names
// cannot be replaced by a Secret reference.
func (p *Policy) EnvVars(labels map[string]string, existing []corev1.EnvVar) ([]corev1.EnvVar, error) {
	labelKeys := make([]string, 0)
	for label := range labels {
		if strings.HasPrefix(label, EnvLabelPrefix) {
			labelKeys = append(labelKeys, label)
		}
	}
	if len(labelKeys) > maxEnvReferences {
		return nil, fmt.Errorf("too many Secret environment references: %d exceeds maximum %d", len(labelKeys), maxEnvReferences)
	}
	sort.Strings(labelKeys)

	existingNames := make(map[string]struct{}, len(existing))
	for _, env := range existing {
		existingNames[env.Name] = struct{}{}
	}

	result := make([]corev1.EnvVar, 0, len(labelKeys))
	for _, label := range labelKeys {
		envName := strings.TrimPrefix(label, EnvLabelPrefix)
		if problems := validation.IsEnvVarName(envName); len(problems) > 0 {
			return nil, fmt.Errorf("invalid Secret environment variable name %q: %s", envName, strings.Join(problems, ", "))
		}
		if _, exists := existingNames[envName]; exists {
			return nil, fmt.Errorf("Secret environment variable %q conflicts with an ordinary environment variable", envName)
		}

		secretName, secretKey, err := p.parseReference(envName, labels[label])
		if err != nil {
			return nil, err
		}

		result = append(result, corev1.EnvVar{
			Name: envName,
			ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
				Key:                  secretKey,
			}},
		})
	}
	return result, nil
}

func (p *Policy) parseReference(target, raw string) (string, string, error) {
	secretName, secretKey, found := strings.Cut(strings.TrimSpace(raw), ":")
	if !found || secretName == "" || secretKey == "" {
		return "", "", fmt.Errorf("invalid Secret reference for %q: expected <secret-name>:<secret-key>", target)
	}
	if problems := validation.IsDNS1123Subdomain(secretName); len(problems) > 0 {
		return "", "", fmt.Errorf("invalid Secret name %q for %q: %s", secretName, target, strings.Join(problems, ", "))
	}
	if problems := validation.IsConfigMapKey(secretKey); len(problems) > 0 {
		return "", "", fmt.Errorf("invalid Secret key %q for %q: %s", secretKey, target, strings.Join(problems, ", "))
	}
	if _, allowed := p.allowed[secretName]; !allowed {
		return "", "", &DeniedError{SecretName: secretName}
	}
	return secretName, secretKey, nil
}
