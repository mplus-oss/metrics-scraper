package main

import (
	"net/http"
	"bufio"
	"strings"
	"io/ioutil"
	"context"

	"github.com/labstack/echo/v4"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/kubernetes"
	apisv1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	kubeStateMetricsURL = "http://kube-state-metrics:8080/metrics"
	nodeExporterEndpoint = "node-exporter"
)

func getNamespace() (string, error) {
	bytefile, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return "", err
	}
	return string(bytefile), nil
}

func getNodeAddresses(clientset *kubernetes.Clientset, namespace string) ([]corev1.EndpointAddress, error) {
	endpoints, err := clientset.CoreV1().Endpoints(namespace).Get(context.TODO(), nodeExporterEndpoint, apisv1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return endpoints.Subsets[0].Addresses, nil
}

func getKubeStateMetrics() (string, error) {
	res, err := http.Get(kubeStateMetricsURL)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	var stateMetrics strings.Builder
	scanner := bufio.NewScanner(res.Body)

	for scanner.Scan() {
		line := scanner.Text()
		if position := strings.Index(line, "{"); position != -1 {
			line = line[:position+1] + "component=\"state\"," + line[position+1:]
		}
		stateMetrics.WriteString(line + "\n")
	}

	if scanner.Err() != nil {
		return "", scanner.Err()
	}

	return stateMetrics.String(), nil
}

func getNodeMetrics(clientset *kubernetes.Clientset, namespace string) (string, error) {
	addresses, err := getNodeAddresses(clientset, namespace)
	if err != nil {
		return "", err
	}
	var allNodeMetrics strings.Builder
	for _, address := range addresses {
		res, err := http.Get("http://" + address.IP + ":9100/metrics")
		if err != nil {
			return "", err
		}
		defer res.Body.Close()
		var metrics strings.Builder
		scanner := bufio.NewScanner(res.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if position := strings.Index(line, "{"); position != -1 {
				line = line[:position+1] + "component=\"node\",node=\"" + *address.NodeName + "\"," + line[position+1:]
			} else {
				if line[0] != '#' {
					if split := strings.Split(line, " "); len(split) == 2 {
						line = split[0] + "{component=\"node\",node=\"" + *address.NodeName + "\"}" + " " + split[1]
					}
				}
			}
			metrics.WriteString(line + "\n")
		}

		if scanner.Err() != nil {
			return "", scanner.Err()
		}

		allNodeMetrics.WriteString(metrics.String())
	}
	return allNodeMetrics.String(), nil
}

func getCAdvisorMetrics(clientset *kubernetes.Clientset, namespace string) (string, error) {
	addresses, err := getNodeAddresses(clientset, namespace)
	if err != nil {
		return "", err
	}
	var allCAdvisorMetrics strings.Builder

	for _, address := range addresses {
		req := clientset.CoreV1().RESTClient().Get().AbsPath("/api/v1/nodes/" + *address.NodeName + "/proxy/metrics/cadvisor")
		res, err := req.Stream(context.TODO())
		if err != nil { 
			return "", err
		}

		var metrics strings.Builder
		scanner := bufio.NewScanner(res)
		for scanner.Scan() {
			line := scanner.Text()
			if position := strings.Index(line, "{"); position != -1 {
				line = line[:position+1] + "component=\"cadvisor\",node=\"" + *address.NodeName + "\"," + line[position+1:]
			}
			metrics.WriteString(line + "\n")
		}

		if scanner.Err() != nil {
			return "", scanner.Err()
		}

		allCAdvisorMetrics.WriteString(metrics.String())
	}

	return allCAdvisorMetrics.String(), nil
}

func main() {
	namespace, err := getNamespace()
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	e := echo.New()
	e.HideBanner = true

	e.GET("/metrics", func(c echo.Context) error {
		kubeStateMetrics, err := getKubeStateMetrics()
		if err != nil {
			return c.String(http.StatusInternalServerError, err.Error())
		}

		nodeMetrics, err := getNodeMetrics(clientset, namespace)
		if err != nil {
			return c.String(http.StatusInternalServerError, err.Error())
		}

		cAdvisorMetrics, err := getCAdvisorMetrics(clientset, namespace)
		if err != nil {
			return c.String(http.StatusInternalServerError, err.Error())
		}
		return c.String(http.StatusOK, kubeStateMetrics + "\n" + nodeMetrics + "\n" + cAdvisorMetrics)
	})

	e.GET("/endpoints", func(c echo.Context) error {
		addresses, err := getNodeAddresses(clientset, namespace)
		if err != nil {
			return c.String(http.StatusInternalServerError, err.Error())
		}
		return c.JSON(http.StatusOK, addresses)
	})
	e.Logger.Fatal(e.Start(":7878"))
}
