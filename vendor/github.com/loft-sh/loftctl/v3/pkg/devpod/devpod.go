package devpod

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/gorilla/websocket"
	managementv1 "github.com/loft-sh/api/v3/pkg/apis/management/v1"
	storagev1 "github.com/loft-sh/api/v3/pkg/apis/storage/v1"
	"github.com/loft-sh/loftctl/v3/pkg/client"
	"github.com/loft-sh/loftctl/v3/pkg/client/naming"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	LoftWorkspaceID       = "WORKSPACE_ID"
	LoftWorkspaceUID      = "WORKSPACE_UID"
	LoftWorkspaceContext  = "WORKSPACE_CONTEXT"
	LoftWorkspaceProvider = "WORKSPACE_PROVIDER"

	LoftProject = "LOFT_PROJECT"

	LoftTemplateOption        = "LOFT_TEMPLATE"
	LoftTemplateVersionOption = "LOFT_TEMPLATE_VERSION"
)

type WorkspaceInfo struct {
	ID          string
	UID         string
	ProjectName string
}

func GetWorkspaceInfoFromEnv() (*WorkspaceInfo, error) {
	workspaceInfo := &WorkspaceInfo{}
	// get workspace id
	workspaceID := os.Getenv(LoftWorkspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("%s is missing in environment", LoftWorkspaceID)
	}
	workspaceInfo.ID = workspaceID

	// get workspace uid
	workspaceUID := os.Getenv(LoftWorkspaceUID)
	if workspaceUID == "" {
		return nil, fmt.Errorf("%s is missing in environment", LoftWorkspaceUID)
	}
	workspaceInfo.UID = workspaceUID

	// get project
	projectName := os.Getenv(LoftProject)
	if projectName == "" {
		return nil, fmt.Errorf("%s is missing in environment", LoftProject)
	}
	workspaceInfo.ProjectName = projectName

	return workspaceInfo, nil
}

func FindWorkspace(ctx context.Context, baseClient client.Client, uid, projectName string) (*managementv1.DevPodWorkspaceInstance, error) {
	// create client
	managementClient, err := baseClient.Management()
	if err != nil {
		return nil, fmt.Errorf("create management client: %w", err)
	}

	// get workspace
	workspaceList, err := managementClient.Loft().ManagementV1().DevPodWorkspaceInstances(naming.ProjectNamespace(projectName)).List(ctx, metav1.ListOptions{
		LabelSelector: storagev1.DevPodWorkspaceUIDLabel + "=" + uid,
	})
	if err != nil {
		return nil, err
	} else if len(workspaceList.Items) == 0 {
		return nil, nil
	}

	return &workspaceList.Items[0], nil
}

func FindWorkspaceByName(ctx context.Context, baseClient client.Client, name, projectName string) (*managementv1.DevPodWorkspaceInstance, error) {
	// create client
	managementClient, err := baseClient.Management()
	if err != nil {
		return nil, fmt.Errorf("create management client: %w", err)
	}

	// get workspace
	workspace, err := managementClient.Loft().ManagementV1().DevPodWorkspaceInstances(naming.ProjectNamespace(projectName)).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return workspace, nil
}

func OptionsFromEnv(name string) url.Values {
	options := os.Getenv(name)
	if options != "" {
		return url.Values{
			"options": []string{options},
		}
	}

	return nil
}

func DialWorkspace(baseClient client.Client, workspace *managementv1.DevPodWorkspaceInstance, subResource string, values url.Values) (*websocket.Conn, error) {
	restConfig, err := baseClient.ManagementConfig()
	if err != nil {
		return nil, err
	}

	host := restConfig.Host
	if workspace.Annotations != nil && workspace.Annotations[storagev1.DevPodWorkspaceRunnerEndpointAnnotation] != "" {
		host = workspace.Annotations[storagev1.DevPodWorkspaceRunnerEndpointAnnotation]
	}

	parsedURL, _ := url.Parse(host)
	if parsedURL != nil && parsedURL.Host != "" {
		host = parsedURL.Host
	}

	loftURL := "wss://" + host + "/kubernetes/management/apis/management.loft.sh/v1/namespaces/" + workspace.Namespace + "/devpodworkspaceinstances/" + workspace.Name + "/" + subResource
	if len(values) > 0 {
		loftURL += "?" + values.Encode()
	}

	dialer := websocket.Dialer{
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: true},
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 45 * time.Second,
	}

	conn, response, err := dialer.Dial(loftURL, map[string][]string{
		"Authorization": {"Bearer " + restConfig.BearerToken},
	})
	if err != nil {
		if response != nil {
			out, _ := io.ReadAll(response.Body)
			headers, _ := json.Marshal(response.Header)
			return nil, fmt.Errorf("error dialing websocket %s (code %d): headers - %s, response - %s, error - %w", loftURL, response.StatusCode, string(headers), string(out), err)
		}

		return nil, fmt.Errorf("error dialing websocket %s: %w", loftURL, err)
	}

	return conn, nil
}