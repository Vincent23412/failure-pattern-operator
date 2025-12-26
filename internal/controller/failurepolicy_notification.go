package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	resiliencev1alpha1 "github.com/Vincent23412/failure-pattern-operator/api/v1alpha1"
)

type FailureNotification struct {
	Policy       string
	Namespace    string
	Target       string
	Action       string
	RestartDelta int
	Message      string
	Timestamp    time.Time
}

func (r *FailurePolicyReconciler) sendNotification(
	ctx context.Context,
	policy *resiliencev1alpha1.FailurePolicy,
	payload FailureNotification,
) error {
	if !policy.Spec.Notification.Enabled {
		return nil
	}

	var secret corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: policy.Namespace,
		Name:      policy.Spec.Notification.Secret,
	}, &secret); err != nil {
		return err
	}

	webhookURL := string(secret.Data["url"])
	if webhookURL == "" {
		return fmt.Errorf("notification secret missing url")
	}

	body, err := buildNotificationBody(policy.Spec.Notification.Type, payload)
	if err != nil {
		return err
	}

	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		webhookURL,
		bytes.NewBuffer(data),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("notification failed: %s", resp.Status)
	}

	return nil
}

func buildNotificationBody(
	notificationType resiliencev1alpha1.NotificationType,
	payload FailureNotification,
) (map[string]string, error) {
	
	switch notificationType {
	case resiliencev1alpha1.NotificationDiscord:
		return buildDiscordBody(payload), nil
	case resiliencev1alpha1.NotificationSlack:
		return buildSlackBody(payload), nil
	default:
		return nil, fmt.Errorf("unsupported notification type: %s", notificationType)
	}
}

func buildDiscordBody(payload FailureNotification) map[string]string {
	content := fmt.Sprintf(
		"**Failure Action Executed**\n"+
			"**Policy:** %s\n"+
			"**Namespace:** %s\n"+
			"**Target:** %s\n"+
			"**Action:** `%s`\n"+
			"**RestartDelta:** `%d`\n"+
			"**Message:** %s\n"+
			"**Time:** %s",
		payload.Policy,
		payload.Namespace,
		payload.Target,
		payload.Action,
		payload.RestartDelta,
		payload.Message,
		payload.Timestamp.Format(time.RFC3339),
	)

	return map[string]string{"content": content}
}

func buildSlackBody(payload FailureNotification) map[string]string {
	fmt.Println("here")
	fmt.Println(payload)
	text := fmt.Sprintf(
		"Failure action executed\n"+
			"Policy: %s\n"+
			"Namespace: %s\n"+
			"Target: %s\n"+
			"Action: %s\n"+
			"RestartDelta: %d\n"+
			"Message: %s\n"+
			"Time: %s",
		payload.Policy,
		payload.Namespace,
		payload.Target,
		payload.Action,
		payload.RestartDelta,
		payload.Message,
		payload.Timestamp.Format(time.RFC3339),
	)

	return map[string]string{"text": text}
}
