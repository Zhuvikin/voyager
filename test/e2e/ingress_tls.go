package e2e

import (
	"fmt"
	"net/http"
	"strings"

	api "github.com/appscode/voyager/apis/voyager/v1beta1"
	"github.com/appscode/voyager/test/framework"
	"github.com/appscode/voyager/test/test-server/client"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var _ = Describe("IngressTLS", func() {
	var (
		f      *framework.Invocation
		ing    *api.Ingress
		secret *core.Secret
	)

	BeforeEach(func() {
		f = root.Invoke()
		ing = f.Ingress.GetSkeleton()
		f.Ingress.SetSkeletonRule(ing)
	})

	BeforeEach(func() {
		crt, key, err := f.CertStore.NewServerCertPair("server", f.ServerSANs())
		Expect(err).NotTo(HaveOccurred())

		secret = &core.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      f.Ingress.UniqueName(),
				Namespace: ing.GetNamespace(),
			},
			Type: core.SecretTypeTLS,
			Data: map[string][]byte{
				core.TLSCertKey:       crt,
				core.TLSPrivateKeyKey: key,
			},
		}
		_, err = f.KubeClient.CoreV1().Secrets(secret.Namespace).Create(secret)
		Expect(err).NotTo(HaveOccurred())
	})

	JustBeforeEach(func() {
		By("Creating ingress with name " + ing.GetName())
		err := f.Ingress.Create(ing)
		Expect(err).NotTo(HaveOccurred())

		f.Ingress.EventuallyStarted(ing).Should(BeTrue())

		By("Checking generated resource")
		Expect(f.Ingress.IsExistsEventually(ing)).Should(BeTrue())
	})

	AfterEach(func() {
		if options.Cleanup {
			f.Ingress.Delete(ing)
			f.KubeClient.CoreV1().Secrets(secret.Namespace).Delete(secret.Name, &metav1.DeleteOptions{})
		}
	})

	var (
		shouldTestHttp = func(port int32, path string) {
			By("Getting HTTP endpoints")
			eps, err := f.Ingress.GetHTTPEndpoints(ing)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(eps)).Should(BeNumerically(">=", 1))

			svc, err := f.Ingress.GetOffShootService(ing)
			Expect(err).NotTo(HaveOccurred())

			var httpPort core.ServicePort
			for _, p := range svc.Spec.Ports {
				if p.Port == port {
					httpPort = p
				}
			}

			Expect(httpPort.Port).Should(Equal(port))

			httpHost := framework.TestDomain
			if ing.UseNodePort() {
				httpHost = framework.TestDomain + ":" + fmt.Sprint(httpPort.NodePort)
			}

			err = f.Ingress.DoHTTP(framework.MaxRetry, httpHost, ing, f.Ingress.FilterEndpointsForPort(eps, httpPort), "GET", path, func(r *client.Response) bool {
				return Expect(r.Status).Should(Equal(http.StatusOK)) &&
					Expect(r.Method).Should(Equal("GET")) &&
					Expect(r.Path).Should(Equal(path))
			})
			Expect(err).NotTo(HaveOccurred())
		}

		shouldTestHttps = func(port int32, path string) {
			By("Getting HTTP endpoints")
			eps, err := f.Ingress.GetHTTPEndpoints(ing)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(eps)).Should(BeNumerically(">=", 1))

			svc, err := f.Ingress.GetOffShootService(ing)
			Expect(err).NotTo(HaveOccurred())

			var httpsPort core.ServicePort
			for _, p := range svc.Spec.Ports {
				if p.Port == port {
					httpsPort = p
				}
			}

			Expect(httpsPort.Port).Should(Equal(port))

			httpsHost := framework.TestDomain
			if ing.UseNodePort() {
				httpsHost = framework.TestDomain + ":" + fmt.Sprint(httpsPort.NodePort)
			}

			err = f.Ingress.DoHTTPs(framework.MaxRetry, httpsHost, "", ing, f.Ingress.FilterEndpointsForPort(eps, httpsPort), "GET", path, func(r *client.Response) bool {
				return Expect(r.Status).Should(Equal(http.StatusOK)) &&
					Expect(r.Method).Should(Equal("GET")) &&
					Expect(r.Path).Should(Equal(path)) &&
					Expect(r.Host).Should(Equal(httpsHost))
			})
			Expect(err).NotTo(HaveOccurred())
		}

		shouldTestRedirect = func(path string) {
			By("Getting HTTP endpoints")
			eps, err := f.Ingress.GetHTTPEndpoints(ing)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(eps)).Should(BeNumerically(">=", 1))

			svc, err := f.Ingress.GetOffShootService(ing)
			Expect(err).NotTo(HaveOccurred())

			var httpPort, httpsPort core.ServicePort
			for _, p := range svc.Spec.Ports {
				if p.Port == 80 {
					httpPort = p
				} else if p.Port == 443 {
					httpsPort = p
				}
			}

			Expect(httpPort.Port).Should(Equal(int32(80)))
			Expect(httpsPort.Port).Should(Equal(int32(443)))

			httpHost, httpsHost := framework.TestDomain, framework.TestDomain
			if ing.UseNodePort() {
				httpHost = framework.TestDomain + ":" + fmt.Sprint(httpPort.NodePort)
				httpsHost = framework.TestDomain + ":" + fmt.Sprint(httpsPort.NodePort)
			}

			err = f.Ingress.DoHTTPTestRedirectWithHost(framework.MaxRetry, httpHost, ing, f.Ingress.FilterEndpointsForPort(eps, httpPort), "GET", path, func(r *client.Response) bool {
				return Expect(r.Status).Should(Equal(308)) &&
					Expect(r.ResponseHeader).Should(HaveKey("Location")) &&
					Expect(r.ResponseHeader.Get("Location")).Should(Equal("https://"+httpsHost+path))
			})
			Expect(err).NotTo(HaveOccurred())
		}
	)

	Describe("Https redirect and response", func() {
		BeforeEach(func() {
			ing.Spec = api.IngressSpec{
				TLS: []api.IngressTLS{
					{
						SecretName: secret.Name,
						Hosts:      []string{framework.TestDomain},
					},
				},
				Rules: []api.IngressRule{
					{
						Host: framework.TestDomain,
						IngressRuleValue: api.IngressRuleValue{
							HTTP: &api.HTTPIngressRuleValue{
								Paths: []api.HTTPIngressPath{
									{
										Path: "/testpath",
										Backend: api.HTTPIngressBackend{
											IngressBackend: api.IngressBackend{
												ServiceName: f.Ingress.TestServerName(),
												ServicePort: intstr.FromInt(80),
											},
										},
									},
								},
							},
						},
					},
				},
			}
		})

		It("Should redirect HTTP to HTTPs and response HTTPs", func() {
			shouldTestHttps(443, "/testpath/ok")
			shouldTestRedirect("/testpath/ok")
		})
	})

	Describe("Redirect with use-nodeport", func() {
		BeforeEach(func() {
			ing.Annotations[api.LBType] = api.LBTypeNodePort
			ing.Annotations[api.UseNodePort] = "true"

			ing.Spec = api.IngressSpec{
				TLS: []api.IngressTLS{
					{
						SecretName: secret.Name,
						Hosts:      []string{framework.TestDomain},
					},
				},
				Rules: []api.IngressRule{
					{
						Host: framework.TestDomain,
						IngressRuleValue: api.IngressRuleValue{
							HTTP: &api.HTTPIngressRuleValue{
								Paths: []api.HTTPIngressPath{
									{
										Path: "/testpath",
										Backend: api.HTTPIngressBackend{
											IngressBackend: api.IngressBackend{
												ServiceName: f.Ingress.TestServerName(),
												ServicePort: intstr.FromInt(80),
											},
										},
									},
								},
							},
						},
					},
				},
			}
		})

		It("Should redirect to HTTPs with correct nodeport", func() {
			shouldTestHttps(443, "/testpath/ok")
			shouldTestRedirect("/testpath/ok")
		})
	})

	Describe("No redirect for existing path", func() {
		BeforeEach(func() {
			ing.Spec = api.IngressSpec{
				TLS: []api.IngressTLS{
					{
						SecretName: secret.Name,
						Hosts:      []string{framework.TestDomain},
					},
				},
				Rules: []api.IngressRule{
					{
						Host: framework.TestDomain,
						IngressRuleValue: api.IngressRuleValue{
							HTTP: &api.HTTPIngressRuleValue{
								Port:  intstr.FromInt(80),
								NoTLS: true,
								Paths: []api.HTTPIngressPath{
									{
										Path: "/testpath",
										Backend: api.HTTPIngressBackend{
											IngressBackend: api.IngressBackend{
												ServiceName: f.Ingress.TestServerName(),
												ServicePort: intstr.FromInt(80),
											},
										},
									},
								},
							},
						},
					},
					{
						Host: framework.TestDomain,
						IngressRuleValue: api.IngressRuleValue{
							HTTP: &api.HTTPIngressRuleValue{
								Port: intstr.FromInt(443),
								Paths: []api.HTTPIngressPath{
									{
										Path: "/testpath",
										Backend: api.HTTPIngressBackend{
											IngressBackend: api.IngressBackend{
												ServiceName: f.Ingress.TestServerName(),
												ServicePort: intstr.FromInt(80),
											},
										},
									},
								},
							},
						},
					},
				},
			}
		})

		It("should response http without redirect for existing path", func() {
			shouldTestHttp(80, "/testpath/ok")
			shouldTestHttps(443, "/testpath/ok")
		})
	})

	Describe("Inject new redirect path", func() {
		BeforeEach(func() {
			ing.Spec = api.IngressSpec{
				TLS: []api.IngressTLS{
					{
						SecretName: secret.Name,
						Hosts:      []string{framework.TestDomain},
					},
				},
				Rules: []api.IngressRule{
					{
						Host: framework.TestDomain,
						IngressRuleValue: api.IngressRuleValue{
							HTTP: &api.HTTPIngressRuleValue{
								Port:  intstr.FromInt(80),
								NoTLS: true,
								Paths: []api.HTTPIngressPath{
									{
										Path: "/alternate",
										Backend: api.HTTPIngressBackend{
											IngressBackend: api.IngressBackend{
												ServiceName: f.Ingress.TestServerName(),
												ServicePort: intstr.FromInt(80),
											},
										},
									},
								},
							},
						},
					},
					{
						Host: framework.TestDomain,
						IngressRuleValue: api.IngressRuleValue{
							HTTP: &api.HTTPIngressRuleValue{
								Port: intstr.FromInt(443),
								Paths: []api.HTTPIngressPath{
									{
										Path: "/testpath",
										Backend: api.HTTPIngressBackend{
											IngressBackend: api.IngressBackend{
												ServiceName: f.Ingress.TestServerName(),
												ServicePort: intstr.FromInt(80),
											},
										},
									},
								},
							},
						},
					},
				},
			}
		})

		It("should inject new redirect path", func() {
			shouldTestHttp(80, "/alternate/ok")
			shouldTestHttps(443, "/testpath/ok")
			shouldTestRedirect("/testpath/ok")
		})
	})

	Describe("Http in port 443", func() {
		BeforeEach(func() {
			ing.Spec = api.IngressSpec{
				TLS: []api.IngressTLS{
					{
						SecretName: secret.Name,
						Hosts:      []string{framework.TestDomain},
					},
				},
				Rules: []api.IngressRule{
					{
						Host: framework.TestDomain,
						IngressRuleValue: api.IngressRuleValue{
							HTTP: &api.HTTPIngressRuleValue{
								NoTLS: true,
								Port:  intstr.FromInt(443),
								Paths: []api.HTTPIngressPath{
									{
										Path: "/testpath",
										Backend: api.HTTPIngressBackend{
											IngressBackend: api.IngressBackend{
												ServiceName: f.Ingress.TestServerName(),
												ServicePort: intstr.FromInt(80),
											},
										},
									},
								},
							},
						},
					},
				},
			}
		})

		It("Should response HTTP from port 443", func() {
			shouldTestHttp(443, "/testpath/ok")
		})
	})

	Describe("SSL Passthrough", func() {
		BeforeEach(func() {
			ing.Annotations[api.SSLPassthrough] = "true"
			ing.Annotations[api.SSLRedirect] = "false"

			ing.Spec = api.IngressSpec{
				TLS: []api.IngressTLS{
					{
						SecretName: secret.Name,
						Hosts:      []string{framework.TestDomain},
					},
				},
				Rules: []api.IngressRule{
					{
						Host: framework.TestDomain,
						IngressRuleValue: api.IngressRuleValue{
							HTTP: &api.HTTPIngressRuleValue{
								Paths: []api.HTTPIngressPath{
									{
										Path: "/testpath",
										Backend: api.HTTPIngressBackend{
											IngressBackend: api.IngressBackend{
												ServiceName: f.Ingress.TestServerName(),
												ServicePort: intstr.FromInt(80),
											},
										},
									},
								},
							},
						},
					},
				},
			}
		})

		It("Should Open 443", func() {
			By("Getting HTTP endpoints")
			eps, err := f.Ingress.GetHTTPEndpoints(ing)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(eps)).Should(BeNumerically(">=", 1))

			svc, err := f.Ingress.GetOffShootService(ing)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(svc.Spec.Ports)).Should(Equal(1))
			Expect(svc.Spec.Ports[0].Port).Should(Equal(int32(443)))

			err = f.Ingress.DoHTTPs(framework.MaxRetry, framework.TestDomain, "", ing, eps, "GET", "/testpath/ok", func(r *client.Response) bool {
				return Expect(r.Status).Should(Equal(http.StatusOK)) &&
					Expect(r.Method).Should(Equal("GET")) &&
					Expect(r.Path).Should(Equal("/testpath/ok")) &&
					Expect(r.Host).Should(Equal(framework.TestDomain))
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("With HSTS Max Age Specified", func() {
		BeforeEach(func() {
			ing.Annotations[api.HSTSMaxAge] = "100"
			ing.Annotations[api.SSLRedirect] = "false"

			ing.Spec = api.IngressSpec{
				TLS: []api.IngressTLS{
					{
						SecretName: secret.Name,
						Hosts:      []string{framework.TestDomain},
					},
				},
				Rules: []api.IngressRule{
					{
						Host: framework.TestDomain,
						IngressRuleValue: api.IngressRuleValue{
							HTTP: &api.HTTPIngressRuleValue{
								Paths: []api.HTTPIngressPath{
									{
										Path: "/testpath",
										Backend: api.HTTPIngressBackend{
											IngressBackend: api.IngressBackend{
												ServiceName: f.Ingress.TestServerName(),
												ServicePort: intstr.FromInt(80),
											},
										},
									},
								},
							},
						},
					},
				},
			}
		})

		It("Should Set max-age Header to Specified Value", func() {
			By("Getting HTTP endpoints")
			eps, err := f.Ingress.GetHTTPEndpoints(ing)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(eps)).Should(BeNumerically(">=", 1))

			svc, err := f.Ingress.GetOffShootService(ing)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(svc.Spec.Ports)).Should(Equal(1))
			Expect(svc.Spec.Ports[0].Port).Should(Equal(int32(443)))

			err = f.Ingress.DoHTTPs(framework.MaxRetry, framework.TestDomain, "", ing, eps, "GET", "/testpath/ok",
				func(r *client.Response) bool {
					return Expect(r.Status).Should(Equal(http.StatusOK)) &&
						Expect(r.Method).Should(Equal("GET")) &&
						Expect(r.Path).Should(Equal("/testpath/ok")) &&
						Expect(r.ResponseHeader.Get("Strict-Transport-Security")).Should(Equal("max-age=100"))
				})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("With HSTS Preload and Subdomains", func() {
		BeforeEach(func() {
			ing.Annotations[api.HSTSPreload] = "true"
			ing.Annotations[api.HSTSIncludeSubDomains] = "true"
			ing.Annotations[api.SSLRedirect] = "false"

			ing.Spec = api.IngressSpec{
				TLS: []api.IngressTLS{
					{
						SecretName: secret.Name,
						Hosts:      []string{framework.TestDomain},
					},
				},
				Rules: []api.IngressRule{
					{
						Host: framework.TestDomain,
						IngressRuleValue: api.IngressRuleValue{
							HTTP: &api.HTTPIngressRuleValue{
								Paths: []api.HTTPIngressPath{
									{
										Path: "/testpath",
										Backend: api.HTTPIngressBackend{
											IngressBackend: api.IngressBackend{
												ServiceName: f.Ingress.TestServerName(),
												ServicePort: intstr.FromInt(80),
											},
										},
									},
								},
							},
						},
					},
				},
			}
		})

		It("Should Add HSTS preload and includeSubDomains Header", func() {
			By("Getting HTTP endpoints")
			eps, err := f.Ingress.GetHTTPEndpoints(ing)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(eps)).Should(BeNumerically(">=", 1))

			svc, err := f.Ingress.GetOffShootService(ing)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(svc.Spec.Ports)).Should(Equal(1))
			Expect(svc.Spec.Ports[0].Port).Should(Equal(int32(443)))

			err = f.Ingress.DoHTTPs(framework.MaxRetry, framework.TestDomain, "", ing, eps, "GET", "/testpath/ok",
				func(r *client.Response) bool {
					return Expect(r.Status).Should(Equal(http.StatusOK)) &&
						Expect(r.Method).Should(Equal("GET")) &&
						Expect(r.Path).Should(Equal("/testpath/ok")) &&
						Expect(r.ResponseHeader.Get("Strict-Transport-Security")).
							Should(Equal("max-age=15768000; preload; includeSubDomains"))
				})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("Force SSL Redirect", func() {
		BeforeEach(func() {
			ing.Annotations[api.ForceSSLRedirect] = "true"
		})

		It("Should redirect HTTP", func() {
			By("Getting HTTP endpoints")
			eps, err := f.Ingress.GetHTTPEndpoints(ing)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(eps)).Should(BeNumerically(">=", 1))

			redirectLocation := "https://" + framework.TestDomain + "/testpath/ok"

			err = f.Ingress.DoHTTPTestRedirectWithHost(framework.MaxRetry, framework.TestDomain, ing, eps, "GET", "/testpath/ok", func(r *client.Response) bool {
				return Expect(r.Status).Should(Equal(308)) &&
					Expect(r.ResponseHeader).Should(HaveKey("Location")) &&
					Expect(r.ResponseHeader.Get("Location")).Should(Equal(redirectLocation))
			})
			Expect(err).NotTo(HaveOccurred())

			err = f.Ingress.DoHTTPTestRedirectWithHeader(framework.MaxRetry, framework.TestDomain, ing, eps, "GET", "/testpath/ok",
				map[string]string{
					"X-Forwarded-Proto": "http",
				},
				func(r *client.Response) bool {
					return Expect(r.Status).Should(Equal(308)) &&
						Expect(r.ResponseHeader).Should(HaveKey("Location")) &&
						Expect(r.ResponseHeader.Get("Location")).Should(Equal(redirectLocation))
				})
			Expect(err).NotTo(HaveOccurred())

			// should not redirect, should response normally
			err = f.Ingress.DoHTTPWithHeader(framework.MaxRetry, ing, eps, "GET", "/testpath/ok",
				map[string]string{
					"X-Forwarded-Proto": "https",
				},
				func(r *client.Response) bool {
					return Expect(r.Status).Should(Equal(200)) &&
						Expect(r.Method).Should(Equal("GET")) &&
						Expect(r.Path).Should(Equal("/testpath/ok"))
				})
			Expect(err).NotTo(HaveOccurred())

			// bad-case: without host header, just replace http with https
			// for-example: http://192.168.99.100:30001 -> https://192.168.99.100:30001
			redirectLocation = strings.Replace(eps[0], "http", "https", 1) + "/testpath/ok"
			err = f.Ingress.DoHTTPTestRedirect(framework.MaxRetry, ing, eps, "GET", "/testpath/ok",
				func(r *client.Response) bool {
					return Expect(r.Status).Should(Equal(308)) &&
						Expect(r.ResponseHeader).Should(HaveKey("Location")) &&
						Expect(r.ResponseHeader.Get("Location")).Should(Equal(redirectLocation))
				})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
