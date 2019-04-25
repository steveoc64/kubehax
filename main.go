package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

var log *logrus.Logger

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}

func main() {
	log = logrus.New()

	log.Println("KubeHax")
	var kubeconfig *string
	if home := homeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()

	// use the current context in kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	//for {
	pods, err := clientset.CoreV1().Pods("").List(metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}
	fmt.Printf("There are %d pods in the cluster\n", len(pods.Items))

	for _, v := range pods.Items {
		log.WithFields(logrus.Fields{
			"name":      v.Name,
			"namespace": v.Namespace,
			"status":    v.Status.Phase,
		}).Println("Pod!")
	}

	// Examples for error handling:
	// - Use helper functions like e.g. errors.IsNotFound()
	// - And/or cast to StatusError and use its properties like e.g. ErrStatus.Message
	namespace := "default"
	i := rand.Intn(len(pods.Items))
	pod := pods.Items[i]
	_, err = clientset.CoreV1().Pods(namespace).Get(pod.Name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		fmt.Printf("Pod %s in namespace %s not found\n", pod.Name, namespace)
	} else if statusError, isStatus := err.(*errors.StatusError); isStatus {
		fmt.Printf("Error getting pod %s in namespace %s: %v\n",
			pod.Name, namespace, statusError.ErrStatus.Message)
	} else if err != nil {
		panic(err.Error())
	} else {
		fmt.Printf("Found pod %s in namespace %s\n", pod.Name, namespace)
	}

	// example adding a watcher
	watchlist := cache.NewListWatchFromClient(clientset.CoreV1().RESTClient(), "pods", "default", fields.Everything())
	_, controller := cache.NewInformer(
		watchlist,
		&v1.Pod{},
		time.Second*0,
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				fmt.Printf("pod added: %s \n", obj)
			},
			DeleteFunc: func(obj interface{}) {
				fmt.Printf("pod deleted: %s \n", obj)
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				p, ok := newObj.(*v1.Pod)
				if ok {
					fmt.Printf("pod changed -> %s:%s:%s:%s\n", p.Name, p.Status.Phase, p.Status.PodIP, p.Status.Message)
					if p.Namespace == "default" && p.Status.Phase == "Running" {
						getPodLogs(clientset, p, time.Second*10)
					}
				}
			},
		},
	)
	stop := make(chan struct{})
	go controller.Run(stop)

	for {
		time.Sleep(time.Second)
	}

	//time.Sleep(10 * time.Second)
	//}
}

func getPodLogs(c *kubernetes.Clientset, pod *v1.Pod, duration time.Duration) {
	podLogOpts := v1.PodLogOptions{Follow: true}
	req := c.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &podLogOpts)
	podLogs, err := req.Stream()
	if err != nil {
		return
	}
	defer println()
	defer podLogs.Close()

	buf := new(bytes.Buffer)
	stopper := time.NewTimer(duration)
	data := make(chan []byte, 4096)

	go func() {
		for {
			if _, err := io.Copy(buf, podLogs); err != nil {
				return
			}
			data <- buf.Bytes()
			buf.Reset()
		}
	}()

	for {
		select {
		case <-stopper.C:
			return
		case bb := <-data:
			ss := string(bb)
			if strings.HasPrefix(ss, "rpc error") {
				return
			}
			print(ss)
		}
	}
}
