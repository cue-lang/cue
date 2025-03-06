package external_secrets

#CustomResourceDefinition: {
	spec: #CustomResourceDefinitionSpec 
}
#ResourceScope: string // #enumResourceScope
#CustomResourceDefinitionSpec: {
	group: string 
	names: #CustomResourceDefinitionNames 
	scope: #ResourceScope 
	versions: [...#CustomResourceDefinitionVersion] 
	conversion?: null | #CustomResourceConversion 
	preserveUnknownFields?: bool 
}
#CustomResourceConversion: {
	// strategy specifies how custom resources are converted between versions. Allowed values are:
	// - `"None"`: The converter only change the apiVersion and would not touch any other field in the custom resource.
	// - `"Webhook"`: API Server will call to an external webhook to do the conversion. Additional information
	//   is needed for this option. This requires spec.preserveUnknownFields to be false, and spec.conversion.webhook to be set.
	strategy: #ConversionStrategyType 

	// webhook describes how to call the conversion webhook. Required when `strategy` is set to `"Webhook"`.
	// +optional
	webhook?: null | #WebhookConversion 
}

#WebhookConversion: {
	clientConfig?: null | #WebhookClientConfig 
	conversionReviewVersions: [...string] 
}
#WebhookClientConfig: {
	url?: null | string 
	service?: null | #ServiceReference 
	caBundle?: bytes 
}
#ServiceReference: {
	namespace: string 
	name: string 
	path?: null | string 
	port?: null | int32 
}
#ConversionStrategyType: string // #enumConversionStrategyType
#CustomResourceDefinitionNames: {
	plural: string 

	singular?: string 

	shortNames?: [...string] 

	kind: string 

	listKind?: string 

	categories?: [...string] 
}
#CustomResourceDefinitionVersion: {
	name: string 

	served: bool 

	storage: bool 

	deprecated?: bool 

	deprecationWarning?: null | string 

	schema?: null | #CustomResourceValidation 

	subresources?: null | #CustomResourceSubresources 

	additionalPrinterColumns?: [...#CustomResourceColumnDefinition] 
}
#CustomResourceColumnDefinition: {
	name: string 

	type: string 

	format?: string 

	description?: string 

	priority?: int32 

	jsonPath: string 
}
#CustomResourceValidation: {
	openAPIV3Schema?: null | #JSONSchemaProps 
}
#CustomResourceSubresources: {
	status?: null | #CustomResourceSubresourceStatus 

	scale?: null | #CustomResourceSubresourceScale 
}
#CustomResourceSubresourceStatus: {
}

#CustomResourceSubresourceScale: {
	specReplicasPath: string 

	statusReplicasPath: string 

	labelSelectorPath?: null | string 
}

#FieldValueErrorReason: string // #enumFieldValueErrorReason

#enumFieldValueErrorReason:
	#FieldValueRequired |
	#FieldValueDuplicate |
	#FieldValueInvalid |
	#FieldValueForbidden
#FieldValueRequired: #FieldValueErrorReason & "FieldValueRequired"
#FieldValueDuplicate: #FieldValueErrorReason & "FieldValueDuplicate"
#FieldValueInvalid: #FieldValueErrorReason & "FieldValueInvalid"
#FieldValueForbidden: #FieldValueErrorReason & "FieldValueForbidden"
#JSONSchemaProps: {
	id?:          string         
	$schema?:     #JSONSchemaURL 
	$ref?:        null | string  
	description?: string         
	type?:        string         
	format?: string 
	title?:  string 
	default?:          null | #JSON   
	maximum?:          null | float64 
	exclusiveMaximum?: bool           
	minimum?:          null | float64 
	exclusiveMinimum?: bool           
	maxLength?:        null | int64   
	minLength?:        null | int64   
	pattern?:          string         
	maxItems?:         null | int64   
	minItems?:         null | int64   
	uniqueItems?:      bool           
	multipleOf?:       null | float64 
	enum?: [...#JSON] 
	maxProperties?: null | int64 
	minProperties?: null | int64 
	required?: [...string] 
	items?: null | #JSONSchemaPropsOrArray 
	allOf?: [...#JSONSchemaProps] 
	oneOf?: [...#JSONSchemaProps] 
	anyOf?: [...#JSONSchemaProps] 
	not?: null | #JSONSchemaProps 
	properties?: {[string]: #JSONSchemaProps} 
	additionalProperties?: null | #JSONSchemaPropsOrBool 
	patternProperties?: {[string]: #JSONSchemaProps} 
	dependencies?:    #JSONSchemaDependencies       
	additionalItems?: null | #JSONSchemaPropsOrBool 
	definitions?:     #JSONSchemaDefinitions        
	externalDocs?:    null | #ExternalDocumentation 
	example?:         null | #JSON                  
	nullable?:        bool                          
	"x-kubernetes-preserve-unknown-fields"?: null | bool 
	"x-kubernetes-embedded-resource"?: bool 
	"x-kubernetes-int-or-string"?: bool 
	"x-kubernetes-list-map-keys"?: [...string] 
	"x-kubernetes-list-type"?: null | string 
	"x-kubernetes-map-type"?: null | string 
	"x-kubernetes-validations"?: #ValidationRules 
}
#ValidationRules: [...#ValidationRule]
#ValidationRule: {
	rule: string 
	message?: string 
	messageExpression?: string 
	reason?: null | #FieldValueErrorReason 
	fieldPath?: string 
	optionalOldSelf?: null | bool 
}
#JSON: _
#JSONSchemaURL: string
#JSONSchemaPropsOrArray: _
#JSONSchemaPropsOrBool: _
#JSONSchemaDependencies: {[string]: #JSONSchemaPropsOrStringArray}
#JSONSchemaPropsOrStringArray: _
#JSONSchemaDefinitions: {[string]: #JSONSchemaProps}
#ExternalDocumentation: {
	description?: string 
	url?:         string 
}

items: [...#CustomResourceDefinition]

items: [{
	spec: {
		group: "generators.external-secrets.io"
		names: {
			categories: ["acraccesstoken"]
			kind:     "ACRAccessToken"
			listKind: "ACRAccessTokenList"
			plural:   "acraccesstokens"
			shortNames: ["acraccesstoken"]
			singular: "acraccesstoken"
		}
		scope: "Namespaced"
		versions: [{
			name: "v1alpha1"
			schema: openAPIV3Schema: {
				description: """
					ACRAccessToken returns a Azure Container Registry token that can be used for pushing/pulling images. Note: by default it will return an ACR Refresh Token with full access (depending on the identity). This can be scoped down to the repository level using .spec.scope. In case scope is defined it will return an ACR Access Token.
					 See docs: https://github.com/Azure/acr/blob/main/docs/AAD-OAuth.md
					"""
				properties: {
					apiVersion: {
						description: "APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources"
						type:        "string"
					}
					kind: {
						description: "Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds"
						type:        "string"
					}
					metadata: type: "object"
					spec: {
						description: "ACRAccessTokenSpec defines how to generate the access token e.g. how to authenticate and which registry to use. see: https://github.com/Azure/acr/blob/main/docs/AAD-OAuth.md#overview"
						properties: {
							auth: {
								properties: {
									managedIdentity: {
										description: "ManagedIdentity uses Azure Managed Identity to authenticate with Azure."
										properties: identityId: {
											description: "If multiple Managed Identity is assigned to the pod, you can select the one to be used"
											type:        "string"
										}
										type: "object"
									}
									servicePrincipal: {
										description: "ServicePrincipal uses Azure Service Principal credentials to authenticate with Azure."
										properties: secretRef: {
											description: "Configuration used to authenticate with Azure using static credentials stored in a Kind=Secret."
											properties: {
												clientId: {
													description: "The Azure clientId of the service principle used for authentication."
													properties: {
														key: {
															description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
															type:        "string"
														}
														name: {
															description: "The name of the Secret resource being referred to."
															type:        "string"
														}
														namespace: {
															description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
															type:        "string"
														}
													}
													type: "object"
												}
												clientSecret: {
													description: "The Azure ClientSecret of the service principle used for authentication."
													properties: {
														key: {
															description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
															type:        "string"
														}
														name: {
															description: "The name of the Secret resource being referred to."
															type:        "string"
														}
														namespace: {
															description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
															type:        "string"
														}
													}
													type: "object"
												}
											}
											type: "object"
										}
										required: [
											"secretRef",
										]
										type: "object"
									}
									workloadIdentity: {
										description: "WorkloadIdentity uses Azure Workload Identity to authenticate with Azure."
										properties: serviceAccountRef: {
											description: "ServiceAccountRef specified the service account that should be used when authenticating with WorkloadIdentity."
											properties: {
												audiences: {
													description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
													items: type: "string"
													type: "array"
												}
												name: {
													description: "The name of the ServiceAccount resource being referred to."
													type:        "string"
												}
												namespace: {
													description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
													type:        "string"
												}
											}
											required: [
												"name",
											]
											type: "object"
										}
										type: "object"
									}
								}
								type: "object"
							}
							environmentType: {
								default:     "PublicCloud"
								description: "EnvironmentType specifies the Azure cloud environment endpoints to use for connecting and authenticating with Azure. By default it points to the public cloud AAD endpoint. The following endpoints are available, also see here: https://github.com/Azure/go-autorest/blob/main/autorest/azure/environments.go#L152 PublicCloud, USGovernmentCloud, ChinaCloud, GermanCloud"
								enum: [
									"PublicCloud",
									"USGovernmentCloud",
									"ChinaCloud",
									"GermanCloud",
								]
								type: "string"
							}
							registry: {
								description: "the domain name of the ACR registry e.g. foobarexample.azurecr.io"
								type:        "string"
							}
							scope: {
								description: """
		Define the scope for the access token, e.g. pull/push access for a repository. if not provided it will return a refresh token that has full scope. Note: you need to pin it down to the repository level, there is no wildcard available.
		 examples: repository:my-repository:pull,push repository:my-repository:pull
		 see docs for details: https://docs.docker.com/registry/spec/auth/scope/
		"""
								type: "string"
							}
							tenantId: {
								description: "TenantID configures the Azure Tenant to send requests to. Required for ServicePrincipal auth type."
								type:        "string"
							}
						}
						required: [
							"auth",
							"registry",
						]
						type: "object"
					}
				}
				type: "object"
			}
			served:  true
			storage: true
			subresources: status: {}
		}]
		conversion: {
			strategy: "Webhook"
			webhook: {
				conversionReviewVersions: [
					"v1",
				]
				clientConfig: service: {
					name:      "external-secrets-webhook"
					namespace: "external-secrets"
					path:      "/convert"
				}
			}
		}
	}
}, {
	spec: {
		group: "external-secrets.io"
		names: {
			categories: ["externalsecrets"]
			kind:     "ClusterExternalSecret"
			listKind: "ClusterExternalSecretList"
			plural:   "clusterexternalsecrets"
			shortNames: ["ces"]
			singular: "clusterexternalsecret"
		}
		scope: "Cluster"
		versions: [{
			additionalPrinterColumns: [{
				jsonPath: ".spec.externalSecretSpec.secretStoreRef.name"
				name:     "Store"
				type:     "string"
			}, {
				jsonPath: ".spec.refreshTime"
				name:     "Refresh Interval"
				type:     "string"
			}, {
				jsonPath: ".status.conditions[?(@.type==\"Ready\")].status"
				name:     "Ready"
				type:     "string"
			}]
			name: "v1beta1"
			schema: openAPIV3Schema: {
				description: "ClusterExternalSecret is the Schema for the clusterexternalsecrets API."
				properties: {
					apiVersion: {
						description: "APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources"
						type:        "string"
					}
					kind: {
						description: "Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds"
						type:        "string"
					}
					metadata: type: "object"
					spec: {
						description: "ClusterExternalSecretSpec defines the desired state of ClusterExternalSecret."
						properties: {
							externalSecretMetadata: {
								description: "The metadata of the external secrets to be created"
								properties: {
									annotations: {
										additionalProperties: type: "string"
										type: "object"
									}
									labels: {
										additionalProperties: type: "string"
										type: "object"
									}
								}
								type: "object"
							}
							externalSecretName: {
								description: "The name of the external secrets to be created defaults to the name of the ClusterExternalSecret"
								type:        "string"
							}
							externalSecretSpec: {
								description: "The spec for the ExternalSecrets to be created"
								properties: {
									data: {
										description: "Data defines the connection between the Kubernetes Secret keys and the Provider data"
										items: {
											description: "ExternalSecretData defines the connection between the Kubernetes Secret key (spec.data.<key>) and the Provider data."
											properties: {
												remoteRef: {
													description: "RemoteRef points to the remote secret and defines which secret (version/property/..) to fetch."
													properties: {
														conversionStrategy: {
															default:     "Default"
															description: "Used to define a conversion Strategy"
															type:        "string"
														}
														decodingStrategy: {
															default:     "None"
															description: "Used to define a decoding Strategy"
															type:        "string"
														}
														key: {
															description: "Key is the key used in the Provider, mandatory"
															type:        "string"
														}
														metadataPolicy: {
															description: "Policy for fetching tags/labels from provider secrets, possible options are Fetch, None. Defaults to None"
															type:        "string"
														}
														property: {
															description: "Used to select a specific property of the Provider value (if a map), if supported"
															type:        "string"
														}
														version: {
															description: "Used to select a specific version of the Provider value, if supported"
															type:        "string"
														}
													}
													required: [
														"key",
													]
													type: "object"
												}
												secretKey: {
													description: "SecretKey defines the key in which the controller stores the value. This is the key in the Kind=Secret"
													type:        "string"
												}
												sourceRef: {
													description:   "SourceRef allows you to override the source from which the value will pulled from."
													maxProperties: 1
													properties: {
														generatorRef: {
															description: "GeneratorRef points to a generator custom resource in"
															properties: {
																apiVersion: {
																	default:     "generators.external-secrets.io/v1alpha1"
																	description: "Specify the apiVersion of the generator resource"
																	type:        "string"
																}
																kind: {
																	description: "Specify the Kind of the resource, e.g. Password, ACRAccessToken etc."
																	type:        "string"
																}
																name: {
																	description: "Specify the name of the generator resource"
																	type:        "string"
																}
															}
															required: [
																"kind",
																"name",
															]
															type: "object"
														}
														storeRef: {
															description: "SecretStoreRef defines which SecretStore to fetch the ExternalSecret data."
															properties: {
																kind: {
																	description: "Kind of the SecretStore resource (SecretStore or ClusterSecretStore) Defaults to `SecretStore`"
																	type:        "string"
																}
																name: {
																	description: "Name of the SecretStore resource"
																	type:        "string"
																}
															}
															required: [
																"name",
															]
															type: "object"
														}
													}
													type: "object"
												}
											}
											required: [
												"remoteRef",
												"secretKey",
											]
											type: "object"
										}
										type: "array"
									}
									dataFrom: {
										description: "DataFrom is used to fetch all properties from a specific Provider data If multiple entries are specified, the Secret keys are merged in the specified order"
										items: {
											properties: {
												extract: {
													description: "Used to extract multiple key/value pairs from one secret Note: Extract does not support sourceRef.Generator or sourceRef.GeneratorRef."
													properties: {
														conversionStrategy: {
															default:     "Default"
															description: "Used to define a conversion Strategy"
															type:        "string"
														}
														decodingStrategy: {
															default:     "None"
															description: "Used to define a decoding Strategy"
															type:        "string"
														}
														key: {
															description: "Key is the key used in the Provider, mandatory"
															type:        "string"
														}
														metadataPolicy: {
															description: "Policy for fetching tags/labels from provider secrets, possible options are Fetch, None. Defaults to None"
															type:        "string"
														}
														property: {
															description: "Used to select a specific property of the Provider value (if a map), if supported"
															type:        "string"
														}
														version: {
															description: "Used to select a specific version of the Provider value, if supported"
															type:        "string"
														}
													}
													required: [
														"key",
													]
													type: "object"
												}
												find: {
													description: "Used to find secrets based on tags or regular expressions Note: Find does not support sourceRef.Generator or sourceRef.GeneratorRef."
													properties: {
														conversionStrategy: {
															default:     "Default"
															description: "Used to define a conversion Strategy"
															type:        "string"
														}
														decodingStrategy: {
															default:     "None"
															description: "Used to define a decoding Strategy"
															type:        "string"
														}
														name: {
															description: "Finds secrets based on the name."
															properties: regexp: {
																description: "Finds secrets base"
																type:        "string"
															}
															type: "object"
														}
														path: {
															description: "A root path to start the find operations."
															type:        "string"
														}
														tags: {
															additionalProperties: type: "string"
															description: "Find secrets based on tags."
															type:        "object"
														}
													}
													type: "object"
												}
												rewrite: {
													description: "Used to rewrite secret Keys after getting them from the secret Provider Multiple Rewrite operations can be provided. They are applied in a layered order (first to last)"
													items: {
														properties: regexp: {
															description: "Used to rewrite with regular expressions. The resulting key will be the output of a regexp.ReplaceAll operation."
															properties: {
																source: {
																	description: "Used to define the regular expression of a re.Compiler."
																	type:        "string"
																}
																target: {
																	description: "Used to define the target pattern of a ReplaceAll operation."
																	type:        "string"
																}
															}
															required: [
																"source",
																"target",
															]
															type: "object"
														}
														type: "object"
													}
													type: "array"
												}
												sourceRef: {
													description:   "SourceRef points to a store or generator which contains secret values ready to use. Use this in combination with Extract or Find pull values out of a specific SecretStore. When sourceRef points to a generator Extract or Find is not supported. The generator returns a static map of values"
													maxProperties: 1
													properties: {
														generatorRef: {
															description: "GeneratorRef points to a generator custom resource in"
															properties: {
																apiVersion: {
																	default:     "generators.external-secrets.io/v1alpha1"
																	description: "Specify the apiVersion of the generator resource"
																	type:        "string"
																}
																kind: {
																	description: "Specify the Kind of the resource, e.g. Password, ACRAccessToken etc."
																	type:        "string"
																}
																name: {
																	description: "Specify the name of the generator resource"
																	type:        "string"
																}
															}
															required: [
																"kind",
																"name",
															]
															type: "object"
														}
														storeRef: {
															description: "SecretStoreRef defines which SecretStore to fetch the ExternalSecret data."
															properties: {
																kind: {
																	description: "Kind of the SecretStore resource (SecretStore or ClusterSecretStore) Defaults to `SecretStore`"
																	type:        "string"
																}
																name: {
																	description: "Name of the SecretStore resource"
																	type:        "string"
																}
															}
															required: [
																"name",
															]
															type: "object"
														}
													}
													type: "object"
												}
											}
											type: "object"
										}
										type: "array"
									}
									refreshInterval: {
										default:     "1h"
										description: "RefreshInterval is the amount of time before the values are read again from the SecretStore provider Valid time units are \"ns\", \"us\" (or \"µs\"), \"ms\", \"s\", \"m\", \"h\" May be set to zero to fetch and create it once. Defaults to 1h."
										type:        "string"
									}
									secretStoreRef: {
										description: "SecretStoreRef defines which SecretStore to fetch the ExternalSecret data."
										properties: {
											kind: {
												description: "Kind of the SecretStore resource (SecretStore or ClusterSecretStore) Defaults to `SecretStore`"
												type:        "string"
											}
											name: {
												description: "Name of the SecretStore resource"
												type:        "string"
											}
										}
										required: ["name"]
										type: "object"
									}
									target: {
										default: {
											creationPolicy: "Owner"
											deletionPolicy: "Retain"
										}
										description: "ExternalSecretTarget defines the Kubernetes Secret to be created There can be only one target per ExternalSecret."
										properties: {
											creationPolicy: {
												default:     "Owner"
												description: "CreationPolicy defines rules on how to create the resulting Secret Defaults to 'Owner'"
												enum: [
													"Owner",
													"Orphan",
													"Merge",
													"None",
												]
												type: "string"
											}
											deletionPolicy: {
												default:     "Retain"
												description: "DeletionPolicy defines rules on how to delete the resulting Secret Defaults to 'Retain'"
												enum: [
													"Delete",
													"Merge",
													"Retain",
												]
												type: "string"
											}
											immutable: {
												description: "Immutable defines if the final secret will be immutable"
												type:        "boolean"
											}
											name: {
												description: "Name defines the name of the Secret resource to be managed This field is immutable Defaults to the .metadata.name of the ExternalSecret resource"
												type:        "string"
											}
											template: {
												description: "Template defines a blueprint for the created Secret resource."
												properties: {
													data: {
														additionalProperties: type: "string"
														type: "object"
													}
													engineVersion: {
														default: "v2"
														type:    "string"
													}
													mergePolicy: {
														default: "Replace"
														type:    "string"
													}
													metadata: {
														description: "ExternalSecretTemplateMetadata defines metadata fields for the Secret blueprint."
														properties: {
															annotations: {
																additionalProperties: type: "string"
																type: "object"
															}
															labels: {
																additionalProperties: type: "string"
																type: "object"
															}
														}
														type: "object"
													}
													templateFrom: {
														items: {
															properties: {
																configMap: {
																	properties: {
																		items: {
																			items: {
																				properties: {
																					key: type: "string"
																					templateAs: {
																						default: "Values"
																						type:    "string"
																					}
																				}
																				required: ["key"]
																				type: "object"
																			}
																			type: "array"
																		}
																		name: type: "string"
																	}
																	required: [
																		"items",
																		"name",
																	]
																	type: "object"
																}
																literal: type: "string"
																secret: {
																	properties: {
																		items: {
																			items: {
																				properties: {
																					key: type: "string"
																					templateAs: {
																						default: "Values"
																						type:    "string"
																					}
																				}
																				required: ["key"]
																				type: "object"
																			}
																			type: "array"
																		}
																		name: type: "string"
																	}
																	required: [
																		"items",
																		"name",
																	]
																	type: "object"
																}
																target: {
																	default: "Data"
																	type:    "string"
																}
															}
															type: "object"
														}
														type: "array"
													}
													type: type: "string"
												}
												type: "object"
											}
										}
										type: "object"
									}
								}
								type: "object"
							}
							namespaceSelector: {
								description: "The labels to select by to find the Namespaces to create the ExternalSecrets in."
								properties: {
									matchExpressions: {
										description: "matchExpressions is a list of label selector requirements. The requirements are ANDed."
										items: {
											description: "A label selector requirement is a selector that contains values, a key, and an operator that relates the key and values."
											properties: {
												key: {
													description: "key is the label key that the selector applies to."
													type:        "string"
												}
												operator: {
													description: "operator represents a key's relationship to a set of values. Valid operators are In, NotIn, Exists and DoesNotExist."
													type:        "string"
												}
												values: {
													description: "values is an array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. This array is replaced during a strategic merge patch."
													items: type: "string"
													type: "array"
												}
											}
											required: [
												"key",
												"operator",
											]
											type: "object"
										}
										type: "array"
									}
									matchLabels: {
										additionalProperties: type: "string"
										description: "matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is \"key\", the operator is \"In\", and the values array contains only \"value\". The requirements are ANDed."
										type:        "object"
									}
								}
								type:                    "object"
								"x-kubernetes-map-type": "atomic"
							}
							refreshTime: {
								description: "The time in which the controller should reconcile it's objects and recheck namespaces for labels."
								type:        "string"
							}
						}
						required: [
							"externalSecretSpec",
							"namespaceSelector",
						]
						type: "object"
					}
					status: {
						description: "ClusterExternalSecretStatus defines the observed state of ClusterExternalSecret."
						properties: {
							conditions: {
								items: {
									properties: {
										message: type: "string"
										status: type:  "string"
										type: type:    "string"
									}
									required: [
										"status",
										"type",
									]
									type: "object"
								}
								type: "array"
							}
							externalSecretName: {
								description: "ExternalSecretName is the name of the ExternalSecrets created by the ClusterExternalSecret"
								type:        "string"
							}
							failedNamespaces: {
								description: "Failed namespaces are the namespaces that failed to apply an ExternalSecret"
								items: {
									description: "ClusterExternalSecretNamespaceFailure represents a failed namespace deployment and it's reason."
									properties: {
										namespace: {
											description: "Namespace is the namespace that failed when trying to apply an ExternalSecret"
											type:        "string"
										}
										reason: {
											description: "Reason is why the ExternalSecret failed to apply to the namespace"
											type:        "string"
										}
									}
									required: ["namespace"]
									type: "object"
								}
								type: "array"
							}
							provisionedNamespaces: {
								description: "ProvisionedNamespaces are the namespaces where the ClusterExternalSecret has secrets"
								items: type: "string"
								type: "array"
							}
						}
						type: "object"
					}
				}
				type: "object"
			}
			served:  true
			storage: true
			subresources: status: {}
		}]
		conversion: {
			strategy: "Webhook"
			webhook: {
				conversionReviewVersions: ["v1"]
				clientConfig: service: {
					name:      "external-secrets-webhook"
					namespace: "external-secrets"
					path:      "/convert"
				}
			}
		}
	}
}, {
	spec: {
		group: "external-secrets.io"
		names: {
			categories: ["externalsecrets"]
			kind:     "ClusterSecretStore"
			listKind: "ClusterSecretStoreList"
			plural:   "clustersecretstores"
			shortNames: ["css"]
			singular: "clustersecretstore"
		}
		scope: "Cluster"
		versions: [{
			additionalPrinterColumns: [{
				jsonPath: ".metadata.creationTimestamp"
				name:     "AGE"
				type:     "date"
			}, {
				jsonPath: ".status.conditions[?(@.type==\"Ready\")].reason"
				name:     "Status"
				type:     "string"
			}]
			deprecated: true
			name:       "v1alpha1"
			schema: openAPIV3Schema: {
				description: "ClusterSecretStore represents a secure external location for storing secrets, which can be referenced as part of `storeRef` fields."
				properties: {
					apiVersion: {
						description: "APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources"
						type:        "string"
					}
					kind: {
						description: "Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds"
						type:        "string"
					}
					metadata: type: "object"
					spec: {
						description: "SecretStoreSpec defines the desired state of SecretStore."
						properties: {
							controller: {
								description: "Used to select the correct ESO controller (think: ingress.ingressClassName) The ESO controller is instantiated with a specific controller name and filters ES based on this property"
								type:        "string"
							}
							provider: {
								description:   "Used to configure the provider. Only one provider may be set"
								maxProperties: 1
								minProperties: 1
								properties: {
									akeyless: {
										description: "Akeyless configures this store to sync secrets using Akeyless Vault provider"
										properties: {
											akeylessGWApiURL: {
												description: "Akeyless GW API Url from which the secrets to be fetched from."
												type:        "string"
											}
											authSecretRef: {
												description: "Auth configures how the operator authenticates with Akeyless."
												properties: {
													kubernetesAuth: {
														description: "Kubernetes authenticates with Akeyless by passing the ServiceAccount token stored in the named Secret resource."
														properties: {
															accessID: {
																description: "the Akeyless Kubernetes auth-method access-id"
																type:        "string"
															}
															k8sConfName: {
																description: "Kubernetes-auth configuration name in Akeyless-Gateway"
																type:        "string"
															}
															secretRef: {
																description: "Optional secret field containing a Kubernetes ServiceAccount JWT used for authenticating with Akeyless. If a name is specified without a key, `token` is the default. If one is not specified, the one bound to the controller will be used."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															serviceAccountRef: {
																description: "Optional service account field containing the name of a kubernetes ServiceAccount. If the service account is specified, the service account secret token JWT will be used for authenticating with Akeyless. If the service account selector is not supplied, the secretRef will be used instead."
																properties: {
																	audiences: {
																		description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
																		items: type: "string"
																		type: "array"
																	}
																	name: {
																		description: "The name of the ServiceAccount resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																required: ["name"]
																type: "object"
															}
														}
														required: [
															"accessID",
															"k8sConfName",
														]
														type: "object"
													}
													secretRef: {
														description: "Reference to a Secret that contains the details to authenticate with Akeyless."
														properties: {
															accessID: {
																description: "The SecretAccessID is used for authentication"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															accessType: {
																description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															accessTypeParam: {
																description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														type: "object"
													}
												}
												type: "object"
											}
											caBundle: {
												description: "PEM/base64 encoded CA bundle used to validate Akeyless Gateway certificate. Only used if the AkeylessGWApiURL URL is using HTTPS protocol. If not set the system root certificates are used to validate the TLS connection."
												format:      "byte"
												type:        "string"
											}
											caProvider: {
												description: "The provider for the CA bundle to use to validate Akeyless Gateway certificate."
												properties: {
													key: {
														description: "The key the value inside of the provider type to use, only used with \"Secret\" type"
														type:        "string"
													}
													name: {
														description: "The name of the object located at the provider type."
														type:        "string"
													}
													namespace: {
														description: "The namespace the Provider type is in."
														type:        "string"
													}
													type: {
														description: "The type of provider to use such as \"Secret\", or \"ConfigMap\"."
														enum: [
															"Secret",
															"ConfigMap",
														]
														type: "string"
													}
												}
												required: [
													"name",
													"type",
												]
												type: "object"
											}
										}
										required: [
											"akeylessGWApiURL",
											"authSecretRef",
										]
										type: "object"
									}
									alibaba: {
										description: "Alibaba configures this store to sync secrets using Alibaba Cloud provider"
										properties: {
											auth: {
												description: "AlibabaAuth contains a secretRef for credentials."
												properties: {
													rrsa: {
														description: "Authenticate against Alibaba using RRSA."
														properties: {
															oidcProviderArn: type:   "string"
															oidcTokenFilePath: type: "string"
															roleArn: type:           "string"
															sessionName: type:       "string"
														}
														required: [
															"oidcProviderArn",
															"oidcTokenFilePath",
															"roleArn",
															"sessionName",
														]
														type: "object"
													}
													secretRef: {
														description: "AlibabaAuthSecretRef holds secret references for Alibaba credentials."
														properties: {
															accessKeyIDSecretRef: {
																description: "The AccessKeyID is used for authentication"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															accessKeySecretSecretRef: {
																description: "The AccessKeySecret is used for authentication"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														required: [
															"accessKeyIDSecretRef",
															"accessKeySecretSecretRef",
														]
														type: "object"
													}
												}
												type: "object"
											}
											regionID: {
												description: "Alibaba Region to be used for the provider"
												type:        "string"
											}
										}
										required: [
											"auth",
											"regionID",
										]
										type: "object"
									}
									aws: {
										description: "AWS configures this store to sync secrets using AWS Secret Manager provider"
										properties: {
											auth: {
												description: "Auth defines the information necessary to authenticate against AWS if not set aws sdk will infer credentials from your environment see: https://docs.aws.amazon.com/sdk-for-go/v1/developer-guide/configuring-sdk.html#specifying-credentials"
												properties: {
													jwt: {
														description: "Authenticate against AWS using service account tokens."
														properties: serviceAccountRef: {
															description: "A reference to a ServiceAccount resource."
															properties: {
																audiences: {
																	description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
																	items: type: "string"
																	type: "array"
																}
																name: {
																	description: "The name of the ServiceAccount resource being referred to."
																	type:        "string"
																}
																namespace: {
																	description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																	type:        "string"
																}
															}
															required: ["name"]
															type: "object"
														}
														type: "object"
													}
													secretRef: {
														description: "AWSAuthSecretRef holds secret references for AWS credentials both AccessKeyID and SecretAccessKey must be defined in order to properly authenticate."
														properties: {
															accessKeyIDSecretRef: {
																description: "The AccessKeyID is used for authentication"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															secretAccessKeySecretRef: {
																description: "The SecretAccessKey is used for authentication"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														type: "object"
													}
												}
												type: "object"
											}
											region: {
												description: "AWS Region to be used for the provider"
												type:        "string"
											}
											role: {
												description: "Role is a Role ARN which the SecretManager provider will assume"
												type:        "string"
											}
											service: {
												description: "Service defines which service should be used to fetch the secrets"
												enum: [
													"SecretsManager",
													"ParameterStore",
												]
												type: "string"
											}
										}
										required: [
											"region",
											"service",
										]
										type: "object"
									}
									azurekv: {
										description: "AzureKV configures this store to sync secrets using Azure Key Vault provider"
										properties: {
											authSecretRef: {
												description: "Auth configures how the operator authenticates with Azure. Required for ServicePrincipal auth type."
												properties: {
													clientId: {
														description: "The Azure clientId of the service principle used for authentication."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
													clientSecret: {
														description: "The Azure ClientSecret of the service principle used for authentication."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
												}
												type: "object"
											}
											authType: {
												default:     "ServicePrincipal"
												description: "Auth type defines how to authenticate to the keyvault service. Valid values are: - \"ServicePrincipal\" (default): Using a service principal (tenantId, clientId, clientSecret) - \"ManagedIdentity\": Using Managed Identity assigned to the pod (see aad-pod-identity)"
												enum: [
													"ServicePrincipal",
													"ManagedIdentity",
													"WorkloadIdentity",
												]
												type: "string"
											}
											identityId: {
												description: "If multiple Managed Identity is assigned to the pod, you can select the one to be used"
												type:        "string"
											}
											serviceAccountRef: {
												description: "ServiceAccountRef specified the service account that should be used when authenticating with WorkloadIdentity."
												properties: {
													audiences: {
														description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
														items: type: "string"
														type: "array"
													}
													name: {
														description: "The name of the ServiceAccount resource being referred to."
														type:        "string"
													}
													namespace: {
														description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
														type:        "string"
													}
												}
												required: ["name"]
												type: "object"
											}
											tenantId: {
												description: "TenantID configures the Azure Tenant to send requests to. Required for ServicePrincipal auth type."
												type:        "string"
											}
											vaultUrl: {
												description: "Vault Url from which the secrets to be fetched from."
												type:        "string"
											}
										}
										required: ["vaultUrl"]
										type: "object"
									}
									fake: {
										description: "Fake configures a store with static key/value pairs"
										properties: data: {
											items: {
												properties: {
													key: type:   "string"
													value: type: "string"
													valueMap: {
														additionalProperties: type: "string"
														type: "object"
													}
													version: type: "string"
												}
												required: ["key"]
												type: "object"
											}
											type: "array"
										}
										required: ["data"]
										type: "object"
									}
									gcpsm: {
										description: "GCPSM configures this store to sync secrets using Google Cloud Platform Secret Manager provider"
										properties: {
											auth: {
												description: "Auth defines the information necessary to authenticate against GCP"
												properties: {
													secretRef: {
														properties: secretAccessKeySecretRef: {
															description: "The SecretAccessKey is used for authentication"
															properties: {
																key: {
																	description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																	type:        "string"
																}
																name: {
																	description: "The name of the Secret resource being referred to."
																	type:        "string"
																}
																namespace: {
																	description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																	type:        "string"
																}
															}
															type: "object"
														}
														type: "object"
													}
													workloadIdentity: {
														properties: {
															clusterLocation: type:  "string"
															clusterName: type:      "string"
															clusterProjectID: type: "string"
															serviceAccountRef: {
																description: "A reference to a ServiceAccount resource."
																properties: {
																	audiences: {
																		description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
																		items: type: "string"
																		type: "array"
																	}
																	name: {
																		description: "The name of the ServiceAccount resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																required: ["name"]
																type: "object"
															}
														}
														required: [
															"clusterLocation",
															"clusterName",
															"serviceAccountRef",
														]
														type: "object"
													}
												}
												type: "object"
											}
											projectID: {
												description: "ProjectID project where secret is located"
												type:        "string"
											}
										}
										type: "object"
									}
									gitlab: {
										description: "GitLab configures this store to sync secrets using GitLab Variables provider"
										properties: {
											auth: {
												description: "Auth configures how secret-manager authenticates with a GitLab instance."
												properties: SecretRef: {
													properties: accessToken: {
														description: "AccessToken is used for authentication."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
													type: "object"
												}
												required: ["SecretRef"]
												type: "object"
											}
											projectID: {
												description: "ProjectID specifies a project where secrets are located."
												type:        "string"
											}
											url: {
												description: "URL configures the GitLab instance URL. Defaults to https://gitlab.com/."
												type:        "string"
											}
										}
										required: ["auth"]
										type: "object"
									}
									ibm: {
										description: "IBM configures this store to sync secrets using IBM Cloud provider"
										properties: {
											auth: {
												description: "Auth configures how secret-manager authenticates with the IBM secrets manager."
												properties: secretRef: {
													properties: secretApiKeySecretRef: {
														description: "The SecretAccessKey is used for authentication"
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
													type: "object"
												}
												required: ["secretRef"]
												type: "object"
											}
											serviceUrl: {
												description: "ServiceURL is the Endpoint URL that is specific to the Secrets Manager service instance"
												type:        "string"
											}
										}
										required: ["auth"]
										type: "object"
									}
									kubernetes: {
										description: "Kubernetes configures this store to sync secrets using a Kubernetes cluster provider"
										properties: {
											auth: {
												description:   "Auth configures how secret-manager authenticates with a Kubernetes instance."
												maxProperties: 1
												minProperties: 1
												properties: {
													cert: {
														description: "has both clientCert and clientKey as secretKeySelector"
														properties: {
															clientCert: {
																description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															clientKey: {
																description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														type: "object"
													}
													serviceAccount: {
														description: "points to a service account that should be used for authentication"
														properties: serviceAccount: {
															description: "A reference to a ServiceAccount resource."
															properties: {
																audiences: {
																	description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
																	items: type: "string"
																	type: "array"
																}
																name: {
																	description: "The name of the ServiceAccount resource being referred to."
																	type:        "string"
																}
																namespace: {
																	description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																	type:        "string"
																}
															}
															required: ["name"]
															type: "object"
														}
														type: "object"
													}
													token: {
														description: "use static token to authenticate with"
														properties: bearerToken: {
															description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
															properties: {
																key: {
																	description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																	type:        "string"
																}
																name: {
																	description: "The name of the Secret resource being referred to."
																	type:        "string"
																}
																namespace: {
																	description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																	type:        "string"
																}
															}
															type: "object"
														}
														type: "object"
													}
												}
												type: "object"
											}
											remoteNamespace: {
												default:     "default"
												description: "Remote namespace to fetch the secrets from"
												type:        "string"
											}
											server: {
												description: "configures the Kubernetes server Address."
												properties: {
													caBundle: {
														description: "CABundle is a base64-encoded CA certificate"
														format:      "byte"
														type:        "string"
													}
													caProvider: {
														description: "see: https://external-secrets.io/v0.4.1/spec/#external-secrets.io/v1alpha1.CAProvider"
														properties: {
															key: {
																description: "The key the value inside of the provider type to use, only used with \"Secret\" type"
																type:        "string"
															}
															name: {
																description: "The name of the object located at the provider type."
																type:        "string"
															}
															namespace: {
																description: "The namespace the Provider type is in."
																type:        "string"
															}
															type: {
																description: "The type of provider to use such as \"Secret\", or \"ConfigMap\"."
																enum: [
																	"Secret",
																	"ConfigMap",
																]
																type: "string"
															}
														}
														required: [
															"name",
															"type",
														]
														type: "object"
													}
													url: {
														default:     "kubernetes.default"
														description: "configures the Kubernetes server Address."
														type:        "string"
													}
												}
												type: "object"
											}
										}
										required: ["auth"]
										type: "object"
									}
									oracle: {
										description: "Oracle configures this store to sync secrets using Oracle Vault provider"
										properties: {
											auth: {
												description: "Auth configures how secret-manager authenticates with the Oracle Vault. If empty, use the instance principal, otherwise the user credentials specified in Auth."
												properties: {
													secretRef: {
														description: "SecretRef to pass through sensitive information."
														properties: {
															fingerprint: {
																description: "Fingerprint is the fingerprint of the API private key."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															privatekey: {
																description: "PrivateKey is the user's API Signing Key in PEM format, used for authentication."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														required: [
															"fingerprint",
															"privatekey",
														]
														type: "object"
													}
													tenancy: {
														description: "Tenancy is the tenancy OCID where user is located."
														type:        "string"
													}
													user: {
														description: "User is an access OCID specific to the account."
														type:        "string"
													}
												}
												required: [
													"secretRef",
													"tenancy",
													"user",
												]
												type: "object"
											}
											region: {
												description: "Region is the region where vault is located."
												type:        "string"
											}
											vault: {
												description: "Vault is the vault's OCID of the specific vault where secret is located."
												type:        "string"
											}
										}
										required: [
											"region",
											"vault",
										]
										type: "object"
									}
									vault: {
										description: "Vault configures this store to sync secrets using Hashi provider"
										properties: {
											auth: {
												description: "Auth configures how secret-manager authenticates with the Vault server."
												properties: {
													appRole: {
														description: "AppRole authenticates with Vault using the App Role auth mechanism, with the role and secret stored in a Kubernetes Secret resource."
														properties: {
															path: {
																default:     "approle"
																description: "Path where the App Role authentication backend is mounted in Vault, e.g: \"approle\""
																type:        "string"
															}
															roleId: {
																description: "RoleID configured in the App Role authentication backend when setting up the authentication backend in Vault."
																type:        "string"
															}
															secretRef: {
																description: "Reference to a key in a Secret that contains the App Role secret used to authenticate with Vault. The `key` field must be specified and denotes which entry within the Secret resource is used as the app role secret."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														required: [
															"path",
															"roleId",
															"secretRef",
														]
														type: "object"
													}
													cert: {
														description: "Cert authenticates with TLS Certificates by passing client certificate, private key and ca certificate Cert authentication method"
														properties: {
															clientCert: {
																description: "ClientCert is a certificate to authenticate using the Cert Vault authentication method"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															secretRef: {
																description: "SecretRef to a key in a Secret resource containing client private key to authenticate with Vault using the Cert authentication method"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														type: "object"
													}
													jwt: {
														description: "Jwt authenticates with Vault by passing role and JWT token using the JWT/OIDC authentication method"
														properties: {
															kubernetesServiceAccountToken: {
																description: "Optional ServiceAccountToken specifies the Kubernetes service account for which to request a token for with the `TokenRequest` API."
																properties: {
																	audiences: {
																		description: "Optional audiences field that will be used to request a temporary Kubernetes service account token for the service account referenced by `serviceAccountRef`. Defaults to a single audience `vault` it not specified."
																		items: type: "string"
																		type: "array"
																	}
																	expirationSeconds: {
																		description: "Optional expiration time in seconds that will be used to request a temporary Kubernetes service account token for the service account referenced by `serviceAccountRef`. Defaults to 10 minutes."
																		format:      "int64"
																		type:        "integer"
																	}
																	serviceAccountRef: {
																		description: "Service account field containing the name of a kubernetes ServiceAccount."
																		properties: {
																			audiences: {
																				description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
																				items: type: "string"
																				type: "array"
																			}
																			name: {
																				description: "The name of the ServiceAccount resource being referred to."
																				type:        "string"
																			}
																			namespace: {
																				description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																				type:        "string"
																			}
																		}
																		required: ["name"]
																		type: "object"
																	}
																}
																required: ["serviceAccountRef"]
																type: "object"
															}
															path: {
																default:     "jwt"
																description: "Path where the JWT authentication backend is mounted in Vault, e.g: \"jwt\""
																type:        "string"
															}
															role: {
																description: "Role is a JWT role to authenticate using the JWT/OIDC Vault authentication method"
																type:        "string"
															}
															secretRef: {
																description: "Optional SecretRef that refers to a key in a Secret resource containing JWT token to authenticate with Vault using the JWT/OIDC authentication method."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														required: ["path"]
														type: "object"
													}
													kubernetes: {
														description: "Kubernetes authenticates with Vault by passing the ServiceAccount token stored in the named Secret resource to the Vault server."
														properties: {
															mountPath: {
																default:     "kubernetes"
																description: "Path where the Kubernetes authentication backend is mounted in Vault, e.g: \"kubernetes\""
																type:        "string"
															}
															role: {
																description: "A required field containing the Vault Role to assume. A Role binds a Kubernetes ServiceAccount with a set of Vault policies."
																type:        "string"
															}
															secretRef: {
																description: "Optional secret field containing a Kubernetes ServiceAccount JWT used for authenticating with Vault. If a name is specified without a key, `token` is the default. If one is not specified, the one bound to the controller will be used."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															serviceAccountRef: {
																description: "Optional service account field containing the name of a kubernetes ServiceAccount. If the service account is specified, the service account secret token JWT will be used for authenticating with Vault. If the service account selector is not supplied, the secretRef will be used instead."
																properties: {
																	audiences: {
																		description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
																		items: type: "string"
																		type: "array"
																	}
																	name: {
																		description: "The name of the ServiceAccount resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																required: ["name"]
																type: "object"
															}
														}
														required: [
															"mountPath",
															"role",
														]
														type: "object"
													}
													ldap: {
														description: "Ldap authenticates with Vault by passing username/password pair using the LDAP authentication method"
														properties: {
															path: {
																default:     "ldap"
																description: "Path where the LDAP authentication backend is mounted in Vault, e.g: \"ldap\""
																type:        "string"
															}
															secretRef: {
																description: "SecretRef to a key in a Secret resource containing password for the LDAP user used to authenticate with Vault using the LDAP authentication method"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															username: {
																description: "Username is a LDAP user name used to authenticate using the LDAP Vault authentication method"
																type:        "string"
															}
														}
														required: [
															"path",
															"username",
														]
														type: "object"
													}
													tokenSecretRef: {
														description: "TokenSecretRef authenticates with Vault by presenting a token."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
												}
												type: "object"
											}
											caBundle: {
												description: "PEM encoded CA bundle used to validate Vault server certificate. Only used if the Server URL is using HTTPS protocol. This parameter is ignored for plain HTTP protocol connection. If not set the system root certificates are used to validate the TLS connection."
												format:      "byte"
												type:        "string"
											}
											caProvider: {
												description: "The provider for the CA bundle to use to validate Vault server certificate."
												properties: {
													key: {
														description: "The key the value inside of the provider type to use, only used with \"Secret\" type"
														type:        "string"
													}
													name: {
														description: "The name of the object located at the provider type."
														type:        "string"
													}
													namespace: {
														description: "The namespace the Provider type is in."
														type:        "string"
													}
													type: {
														description: "The type of provider to use such as \"Secret\", or \"ConfigMap\"."
														enum: [
															"Secret",
															"ConfigMap",
														]
														type: "string"
													}
												}
												required: [
													"name",
													"type",
												]
												type: "object"
											}
											forwardInconsistent: {
												description: "ForwardInconsistent tells Vault to forward read-after-write requests to the Vault leader instead of simply retrying within a loop. This can increase performance if the option is enabled serverside. https://www.vaultproject.io/docs/configuration/replication#allow_forwarding_via_header"
												type:        "boolean"
											}
											namespace: {
												description: "Name of the vault namespace. Namespaces is a set of features within Vault Enterprise that allows Vault environments to support Secure Multi-tenancy. e.g: \"ns1\". More about namespaces can be found here https://www.vaultproject.io/docs/enterprise/namespaces"
												type:        "string"
											}
											path: {
												description: "Path is the mount path of the Vault KV backend endpoint, e.g: \"secret\". The v2 KV secret engine version specific \"/data\" path suffix for fetching secrets from Vault is optional and will be appended if not present in specified path."
												type:        "string"
											}
											readYourWrites: {
												description: "ReadYourWrites ensures isolated read-after-write semantics by providing discovered cluster replication states in each request. More information about eventual consistency in Vault can be found here https://www.vaultproject.io/docs/enterprise/consistency"
												type:        "boolean"
											}
											server: {
												description: "Server is the connection address for the Vault server, e.g: \"https://vault.example.com:8200\"."
												type:        "string"
											}
											version: {
												default:     "v2"
												description: "Version is the Vault KV secret engine version. This can be either \"v1\" or \"v2\". Version defaults to \"v2\"."
												enum: [
													"v1",
													"v2",
												]
												type: "string"
											}
										}
										required: [
											"auth",
											"server",
										]
										type: "object"
									}
									webhook: {
										description: "Webhook configures this store to sync secrets using a generic templated webhook"
										properties: {
											body: {
												description: "Body"
												type:        "string"
											}
											caBundle: {
												description: "PEM encoded CA bundle used to validate webhook server certificate. Only used if the Server URL is using HTTPS protocol. This parameter is ignored for plain HTTP protocol connection. If not set the system root certificates are used to validate the TLS connection."
												format:      "byte"
												type:        "string"
											}
											caProvider: {
												description: "The provider for the CA bundle to use to validate webhook server certificate."
												properties: {
													key: {
														description: "The key the value inside of the provider type to use, only used with \"Secret\" type"
														type:        "string"
													}
													name: {
														description: "The name of the object located at the provider type."
														type:        "string"
													}
													namespace: {
														description: "The namespace the Provider type is in."
														type:        "string"
													}
													type: {
														description: "The type of provider to use such as \"Secret\", or \"ConfigMap\"."
														enum: [
															"Secret",
															"ConfigMap",
														]
														type: "string"
													}
												}
												required: [
													"name",
													"type",
												]
												type: "object"
											}
											headers: {
												additionalProperties: type: "string"
												description: "Headers"
												type:        "object"
											}
											method: {
												description: "Webhook Method"
												type:        "string"
											}
											result: {
												description: "Result formatting"
												properties: jsonPath: {
													description: "Json path of return value"
													type:        "string"
												}
												type: "object"
											}
											secrets: {
												description: "Secrets to fill in templates These secrets will be passed to the templating function as key value pairs under the given name"
												items: {
													properties: {
														name: {
															description: "Name of this secret in templates"
															type:        "string"
														}
														secretRef: {
															description: "Secret ref to fill in credentials"
															properties: {
																key: {
																	description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																	type:        "string"
																}
																name: {
																	description: "The name of the Secret resource being referred to."
																	type:        "string"
																}
																namespace: {
																	description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																	type:        "string"
																}
															}
															type: "object"
														}
													}
													required: [
														"name",
														"secretRef",
													]
													type: "object"
												}
												type: "array"
											}
											timeout: {
												description: "Timeout"
												type:        "string"
											}
											url: {
												description: "Webhook url to call"
												type:        "string"
											}
										}
										required: [
											"result",
											"url",
										]
										type: "object"
									}
									yandexlockbox: {
										description: "YandexLockbox configures this store to sync secrets using Yandex Lockbox provider"
										properties: {
											apiEndpoint: {
												description: "Yandex.Cloud API endpoint (e.g. 'api.cloud.yandex.net:443')"
												type:        "string"
											}
											auth: {
												description: "Auth defines the information necessary to authenticate against Yandex Lockbox"
												properties: authorizedKeySecretRef: {
													description: "The authorized key used for authentication"
													properties: {
														key: {
															description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
															type:        "string"
														}
														name: {
															description: "The name of the Secret resource being referred to."
															type:        "string"
														}
														namespace: {
															description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
															type:        "string"
														}
													}
													type: "object"
												}
												type: "object"
											}
											caProvider: {
												description: "The provider for the CA bundle to use to validate Yandex.Cloud server certificate."
												properties: certSecretRef: {
													description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
													properties: {
														key: {
															description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
															type:        "string"
														}
														name: {
															description: "The name of the Secret resource being referred to."
															type:        "string"
														}
														namespace: {
															description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
															type:        "string"
														}
													}
													type: "object"
												}
												type: "object"
											}
										}
										required: ["auth"]
										type: "object"
									}
								}
								type: "object"
							}
							retrySettings: {
								description: "Used to configure http retries if failed"
								properties: {
									maxRetries: {
										format: "int32"
										type:   "integer"
									}
									retryInterval: type: "string"
								}
								type: "object"
							}
						}
						required: ["provider"]
						type: "object"
					}
					status: {
						description: "SecretStoreStatus defines the observed state of the SecretStore."
						properties: conditions: {
							items: {
								properties: {
									lastTransitionTime: {
										format: "date-time"
										type:   "string"
									}
									message: type: "string"
									reason: type:  "string"
									status: type:  "string"
									type: type:    "string"
								}
								required: [
									"status",
									"type",
								]
								type: "object"
							}
							type: "array"
						}
						type: "object"
					}
				}
				type: "object"
			}
			served:  true
			storage: false
			subresources: status: {}
		}, {
			additionalPrinterColumns: [{
				jsonPath: ".metadata.creationTimestamp"
				name:     "AGE"
				type:     "date"
			}, {
				jsonPath: ".status.conditions[?(@.type==\"Ready\")].reason"
				name:     "Status"
				type:     "string"
			}, {
				jsonPath: ".status.capabilities"
				name:     "Capabilities"
				type:     "string"
			}, {
				jsonPath: ".status.conditions[?(@.type==\"Ready\")].status"
				name:     "Ready"
				type:     "string"
			}]
			name: "v1beta1"
			schema: openAPIV3Schema: {
				description: "ClusterSecretStore represents a secure external location for storing secrets, which can be referenced as part of `storeRef` fields."
				properties: {
					apiVersion: {
						description: "APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources"
						type:        "string"
					}
					kind: {
						description: "Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds"
						type:        "string"
					}
					metadata: type: "object"
					spec: {
						description: "SecretStoreSpec defines the desired state of SecretStore."
						properties: {
							conditions: {
								description: "Used to constraint a ClusterSecretStore to specific namespaces. Relevant only to ClusterSecretStore"
								items: {
									description: "ClusterSecretStoreCondition describes a condition by which to choose namespaces to process ExternalSecrets in for a ClusterSecretStore instance."
									properties: {
										namespaceSelector: {
											description: "Choose namespace using a labelSelector"
											properties: {
												matchExpressions: {
													description: "matchExpressions is a list of label selector requirements. The requirements are ANDed."
													items: {
														description: "A label selector requirement is a selector that contains values, a key, and an operator that relates the key and values."
														properties: {
															key: {
																description: "key is the label key that the selector applies to."
																type:        "string"
															}
															operator: {
																description: "operator represents a key's relationship to a set of values. Valid operators are In, NotIn, Exists and DoesNotExist."
																type:        "string"
															}
															values: {
																description: "values is an array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. This array is replaced during a strategic merge patch."
																items: type: "string"
																type: "array"
															}
														}
														required: [
															"key",
															"operator",
														]
														type: "object"
													}
													type: "array"
												}
												matchLabels: {
													additionalProperties: type: "string"
													description: "matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is \"key\", the operator is \"In\", and the values array contains only \"value\". The requirements are ANDed."
													type:        "object"
												}
											}
											type:                    "object"
											"x-kubernetes-map-type": "atomic"
										}
										namespaces: {
											description: "Choose namespaces by name"
											items: type: "string"
											type: "array"
										}
									}
									type: "object"
								}
								type: "array"
							}
							controller: {
								description: "Used to select the correct ESO controller (think: ingress.ingressClassName) The ESO controller is instantiated with a specific controller name and filters ES based on this property"
								type:        "string"
							}
							provider: {
								description:   "Used to configure the provider. Only one provider may be set"
								maxProperties: 1
								minProperties: 1
								properties: {
									akeyless: {
										description: "Akeyless configures this store to sync secrets using Akeyless Vault provider"
										properties: {
											akeylessGWApiURL: {
												description: "Akeyless GW API Url from which the secrets to be fetched from."
												type:        "string"
											}
											authSecretRef: {
												description: "Auth configures how the operator authenticates with Akeyless."
												properties: {
													kubernetesAuth: {
														description: "Kubernetes authenticates with Akeyless by passing the ServiceAccount token stored in the named Secret resource."
														properties: {
															accessID: {
																description: "the Akeyless Kubernetes auth-method access-id"
																type:        "string"
															}
															k8sConfName: {
																description: "Kubernetes-auth configuration name in Akeyless-Gateway"
																type:        "string"
															}
															secretRef: {
																description: "Optional secret field containing a Kubernetes ServiceAccount JWT used for authenticating with Akeyless. If a name is specified without a key, `token` is the default. If one is not specified, the one bound to the controller will be used."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															serviceAccountRef: {
																description: "Optional service account field containing the name of a kubernetes ServiceAccount. If the service account is specified, the service account secret token JWT will be used for authenticating with Akeyless. If the service account selector is not supplied, the secretRef will be used instead."
																properties: {
																	audiences: {
																		description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
																		items: type: "string"
																		type: "array"
																	}
																	name: {
																		description: "The name of the ServiceAccount resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																required: ["name"]
																type: "object"
															}
														}
														required: [
															"accessID",
															"k8sConfName",
														]
														type: "object"
													}
													secretRef: {
														description: "Reference to a Secret that contains the details to authenticate with Akeyless."
														properties: {
															accessID: {
																description: "The SecretAccessID is used for authentication"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															accessType: {
																description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															accessTypeParam: {
																description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														type: "object"
													}
												}
												type: "object"
											}
											caBundle: {
												description: "PEM/base64 encoded CA bundle used to validate Akeyless Gateway certificate. Only used if the AkeylessGWApiURL URL is using HTTPS protocol. If not set the system root certificates are used to validate the TLS connection."
												format:      "byte"
												type:        "string"
											}
											caProvider: {
												description: "The provider for the CA bundle to use to validate Akeyless Gateway certificate."
												properties: {
													key: {
														description: "The key where the CA certificate can be found in the Secret or ConfigMap."
														type:        "string"
													}
													name: {
														description: "The name of the object located at the provider type."
														type:        "string"
													}
													namespace: {
														description: "The namespace the Provider type is in. Can only be defined when used in a ClusterSecretStore."
														type:        "string"
													}
													type: {
														description: "The type of provider to use such as \"Secret\", or \"ConfigMap\"."
														enum: [
															"Secret",
															"ConfigMap",
														]
														type: "string"
													}
												}
												required: [
													"name",
													"type",
												]
												type: "object"
											}
										}
										required: [
											"akeylessGWApiURL",
											"authSecretRef",
										]
										type: "object"
									}
									alibaba: {
										description: "Alibaba configures this store to sync secrets using Alibaba Cloud provider"
										properties: {
											auth: {
												description: "AlibabaAuth contains a secretRef for credentials."
												properties: {
													rrsa: {
														description: "Authenticate against Alibaba using RRSA."
														properties: {
															oidcProviderArn: type:   "string"
															oidcTokenFilePath: type: "string"
															roleArn: type:           "string"
															sessionName: type:       "string"
														}
														required: [
															"oidcProviderArn",
															"oidcTokenFilePath",
															"roleArn",
															"sessionName",
														]
														type: "object"
													}
													secretRef: {
														description: "AlibabaAuthSecretRef holds secret references for Alibaba credentials."
														properties: {
															accessKeyIDSecretRef: {
																description: "The AccessKeyID is used for authentication"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															accessKeySecretSecretRef: {
																description: "The AccessKeySecret is used for authentication"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														required: [
															"accessKeyIDSecretRef",
															"accessKeySecretSecretRef",
														]
														type: "object"
													}
												}
												type: "object"
											}
											regionID: {
												description: "Alibaba Region to be used for the provider"
												type:        "string"
											}
										}
										required: [
											"auth",
											"regionID",
										]
										type: "object"
									}
									aws: {
										description: "AWS configures this store to sync secrets using AWS Secret Manager provider"
										properties: {
											additionalRoles: {
												description: "AdditionalRoles is a chained list of Role ARNs which the SecretManager provider will sequentially assume before assuming Role"
												items: type: "string"
												type: "array"
											}
											auth: {
												description: "Auth defines the information necessary to authenticate against AWS if not set aws sdk will infer credentials from your environment see: https://docs.aws.amazon.com/sdk-for-go/v1/developer-guide/configuring-sdk.html#specifying-credentials"
												properties: {
													jwt: {
														description: "Authenticate against AWS using service account tokens."
														properties: serviceAccountRef: {
															description: "A reference to a ServiceAccount resource."
															properties: {
																audiences: {
																	description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
																	items: type: "string"
																	type: "array"
																}
																name: {
																	description: "The name of the ServiceAccount resource being referred to."
																	type:        "string"
																}
																namespace: {
																	description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																	type:        "string"
																}
															}
															required: ["name"]
															type: "object"
														}
														type: "object"
													}
													secretRef: {
														description: "AWSAuthSecretRef holds secret references for AWS credentials both AccessKeyID and SecretAccessKey must be defined in order to properly authenticate."
														properties: {
															accessKeyIDSecretRef: {
																description: "The AccessKeyID is used for authentication"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															secretAccessKeySecretRef: {
																description: "The SecretAccessKey is used for authentication"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															sessionTokenSecretRef: {
																description: "The SessionToken used for authentication This must be defined if AccessKeyID and SecretAccessKey are temporary credentials see: https://docs.aws.amazon.com/IAM/latest/UserGuide/id_credentials_temp_use-resources.html"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														type: "object"
													}
												}
												type: "object"
											}
											externalID: {
												description: "AWS External ID set on assumed IAM roles"
												type:        "string"
											}
											region: {
												description: "AWS Region to be used for the provider"
												type:        "string"
											}
											role: {
												description: "Role is a Role ARN which the SecretManager provider will assume"
												type:        "string"
											}
											service: {
												description: "Service defines which service should be used to fetch the secrets"
												enum: [
													"SecretsManager",
													"ParameterStore",
												]
												type: "string"
											}
											sessionTags: {
												description: "AWS STS assume role session tags"
												items: {
													properties: {
														key: type:   "string"
														value: type: "string"
													}
													required: [
														"key",
														"value",
													]
													type: "object"
												}
												type: "array"
											}
											transitiveTagKeys: {
												description: "AWS STS assume role transitive session tags. Required when multiple rules are used with SecretStore"
												items: type: "string"
												type: "array"
											}
										}
										required: [
											"region",
											"service",
										]
										type: "object"
									}
									azurekv: {
										description: "AzureKV configures this store to sync secrets using Azure Key Vault provider"
										properties: {
											authSecretRef: {
												description: "Auth configures how the operator authenticates with Azure. Required for ServicePrincipal auth type."
												properties: {
													clientId: {
														description: "The Azure clientId of the service principle used for authentication."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
													clientSecret: {
														description: "The Azure ClientSecret of the service principle used for authentication."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
												}
												type: "object"
											}
											authType: {
												default:     "ServicePrincipal"
												description: "Auth type defines how to authenticate to the keyvault service. Valid values are: - \"ServicePrincipal\" (default): Using a service principal (tenantId, clientId, clientSecret) - \"ManagedIdentity\": Using Managed Identity assigned to the pod (see aad-pod-identity)"
												enum: [
													"ServicePrincipal",
													"ManagedIdentity",
													"WorkloadIdentity",
												]
												type: "string"
											}
											environmentType: {
												default:     "PublicCloud"
												description: "EnvironmentType specifies the Azure cloud environment endpoints to use for connecting and authenticating with Azure. By default it points to the public cloud AAD endpoint. The following endpoints are available, also see here: https://github.com/Azure/go-autorest/blob/main/autorest/azure/environments.go#L152 PublicCloud, USGovernmentCloud, ChinaCloud, GermanCloud"
												enum: [
													"PublicCloud",
													"USGovernmentCloud",
													"ChinaCloud",
													"GermanCloud",
												]
												type: "string"
											}
											identityId: {
												description: "If multiple Managed Identity is assigned to the pod, you can select the one to be used"
												type:        "string"
											}
											serviceAccountRef: {
												description: "ServiceAccountRef specified the service account that should be used when authenticating with WorkloadIdentity."
												properties: {
													audiences: {
														description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
														items: type: "string"
														type: "array"
													}
													name: {
														description: "The name of the ServiceAccount resource being referred to."
														type:        "string"
													}
													namespace: {
														description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
														type:        "string"
													}
												}
												required: ["name"]
												type: "object"
											}
											tenantId: {
												description: "TenantID configures the Azure Tenant to send requests to. Required for ServicePrincipal auth type."
												type:        "string"
											}
											vaultUrl: {
												description: "Vault Url from which the secrets to be fetched from."
												type:        "string"
											}
										}
										required: ["vaultUrl"]
										type: "object"
									}
									conjur: {
										description: "Conjur configures this store to sync secrets using conjur provider"
										properties: {
											auth: {
												properties: apikey: {
													properties: {
														account: type: "string"
														apiKeyRef: {
															description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
															properties: {
																key: {
																	description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																	type:        "string"
																}
																name: {
																	description: "The name of the Secret resource being referred to."
																	type:        "string"
																}
																namespace: {
																	description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																	type:        "string"
																}
															}
															type: "object"
														}
														userRef: {
															description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
															properties: {
																key: {
																	description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																	type:        "string"
																}
																name: {
																	description: "The name of the Secret resource being referred to."
																	type:        "string"
																}
																namespace: {
																	description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																	type:        "string"
																}
															}
															type: "object"
														}
													}
													required: [
														"account",
														"apiKeyRef",
														"userRef",
													]
													type: "object"
												}
												required: ["apikey"]
												type: "object"
											}
											caBundle: type: "string"
											url: type:      "string"
										}
										required: [
											"auth",
											"url",
										]
										type: "object"
									}
									delinea: {
										description: "Delinea DevOps Secrets Vault https://docs.delinea.com/online-help/products/devops-secrets-vault/current"
										properties: {
											clientId: {
												description: "ClientID is the non-secret part of the credential."
												properties: {
													secretRef: {
														description: "SecretRef references a key in a secret that will be used as value."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
													value: {
														description: "Value can be specified directly to set a value without using a secret."
														type:        "string"
													}
												}
												type: "object"
											}
											clientSecret: {
												description: "ClientSecret is the secret part of the credential."
												properties: {
													secretRef: {
														description: "SecretRef references a key in a secret that will be used as value."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
													value: {
														description: "Value can be specified directly to set a value without using a secret."
														type:        "string"
													}
												}
												type: "object"
											}
											tenant: {
												description: "Tenant is the chosen hostname / site name."
												type:        "string"
											}
											tld: {
												description: "TLD is based on the server location that was chosen during provisioning. If unset, defaults to \"com\"."
												type:        "string"
											}
											urlTemplate: {
												description: "URLTemplate If unset, defaults to \"https://%s.secretsvaultcloud.%s/v1/%s%s\"."
												type:        "string"
											}
										}
										required: [
											"clientId",
											"clientSecret",
											"tenant",
										]
										type: "object"
									}
									doppler: {
										description: "Doppler configures this store to sync secrets using the Doppler provider"
										properties: {
											auth: {
												description: "Auth configures how the Operator authenticates with the Doppler API"
												properties: secretRef: {
													properties: dopplerToken: {
														description: "The DopplerToken is used for authentication. See https://docs.doppler.com/reference/api#authentication for auth token types. The Key attribute defaults to dopplerToken if not specified."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
													required: ["dopplerToken"]
													type: "object"
												}
												required: ["secretRef"]
												type: "object"
											}
											config: {
												description: "Doppler config (required if not using a Service Token)"
												type:        "string"
											}
											format: {
												description: "Format enables the downloading of secrets as a file (string)"
												enum: [
													"json",
													"dotnet-json",
													"env",
													"yaml",
													"docker",
												]
												type: "string"
											}
											nameTransformer: {
												description: "Environment variable compatible name transforms that change secret names to a different format"
												enum: [
													"upper-camel",
													"camel",
													"lower-snake",
													"tf-var",
													"dotnet-env",
													"lower-kebab",
												]
												type: "string"
											}
											project: {
												description: "Doppler project (required if not using a Service Token)"
												type:        "string"
											}
										}
										required: ["auth"]
										type: "object"
									}
									fake: {
										description: "Fake configures a store with static key/value pairs"
										properties: data: {
											items: {
												properties: {
													key: type:   "string"
													value: type: "string"
													valueMap: {
														additionalProperties: type: "string"
														type: "object"
													}
													version: type: "string"
												}
												required: ["key"]
												type: "object"
											}
											type: "array"
										}
										required: ["data"]
										type: "object"
									}
									gcpsm: {
										description: "GCPSM configures this store to sync secrets using Google Cloud Platform Secret Manager provider"
										properties: {
											auth: {
												description: "Auth defines the information necessary to authenticate against GCP"
												properties: {
													secretRef: {
														properties: secretAccessKeySecretRef: {
															description: "The SecretAccessKey is used for authentication"
															properties: {
																key: {
																	description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																	type:        "string"
																}
																name: {
																	description: "The name of the Secret resource being referred to."
																	type:        "string"
																}
																namespace: {
																	description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																	type:        "string"
																}
															}
															type: "object"
														}
														type: "object"
													}
													workloadIdentity: {
														properties: {
															clusterLocation: type:  "string"
															clusterName: type:      "string"
															clusterProjectID: type: "string"
															serviceAccountRef: {
																description: "A reference to a ServiceAccount resource."
																properties: {
																	audiences: {
																		description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
																		items: type: "string"
																		type: "array"
																	}
																	name: {
																		description: "The name of the ServiceAccount resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																required: ["name"]
																type: "object"
															}
														}
														required: [
															"clusterLocation",
															"clusterName",
															"serviceAccountRef",
														]
														type: "object"
													}
												}
												type: "object"
											}
											projectID: {
												description: "ProjectID project where secret is located"
												type:        "string"
											}
										}
										type: "object"
									}
									gitlab: {
										description: "GitLab configures this store to sync secrets using GitLab Variables provider"
										properties: {
											auth: {
												description: "Auth configures how secret-manager authenticates with a GitLab instance."
												properties: SecretRef: {
													properties: accessToken: {
														description: "AccessToken is used for authentication."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
													type: "object"
												}
												required: ["SecretRef"]
												type: "object"
											}
											environment: {
												description: "Environment environment_scope of gitlab CI/CD variables (Please see https://docs.gitlab.com/ee/ci/environments/#create-a-static-environment on how to create environments)"
												type:        "string"
											}
											groupIDs: {
												description: "GroupIDs specify, which gitlab groups to pull secrets from. Group secrets are read from left to right followed by the project variables."
												items: type: "string"
												type: "array"
											}
											inheritFromGroups: {
												description: "InheritFromGroups specifies whether parent groups should be discovered and checked for secrets."
												type:        "boolean"
											}
											projectID: {
												description: "ProjectID specifies a project where secrets are located."
												type:        "string"
											}
											url: {
												description: "URL configures the GitLab instance URL. Defaults to https://gitlab.com/."
												type:        "string"
											}
										}
										required: ["auth"]
										type: "object"
									}
									ibm: {
										description: "IBM configures this store to sync secrets using IBM Cloud provider"
										properties: {
											auth: {
												description:   "Auth configures how secret-manager authenticates with the IBM secrets manager."
												maxProperties: 1
												minProperties: 1
												properties: {
													containerAuth: {
														description: "IBM Container-based auth with IAM Trusted Profile."
														properties: {
															iamEndpoint: type: "string"
															profile: {
																description: "the IBM Trusted Profile"
																type:        "string"
															}
															tokenLocation: {
																description: "Location the token is mounted on the pod"
																type:        "string"
															}
														}
														required: ["profile"]
														type: "object"
													}
													secretRef: {
														properties: secretApiKeySecretRef: {
															description: "The SecretAccessKey is used for authentication"
															properties: {
																key: {
																	description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																	type:        "string"
																}
																name: {
																	description: "The name of the Secret resource being referred to."
																	type:        "string"
																}
																namespace: {
																	description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																	type:        "string"
																}
															}
															type: "object"
														}
														type: "object"
													}
												}
												type: "object"
											}
											serviceUrl: {
												description: "ServiceURL is the Endpoint URL that is specific to the Secrets Manager service instance"
												type:        "string"
											}
										}
										required: ["auth"]
										type: "object"
									}
									keepersecurity: {
										description: "KeeperSecurity configures this store to sync secrets using the KeeperSecurity provider"
										properties: {
											authRef: {
												description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
												properties: {
													key: {
														description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
														type:        "string"
													}
													name: {
														description: "The name of the Secret resource being referred to."
														type:        "string"
													}
													namespace: {
														description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
														type:        "string"
													}
												}
												type: "object"
											}
											folderID: type: "string"
										}
										required: [
											"authRef",
											"folderID",
										]
										type: "object"
									}
									kubernetes: {
										description: "Kubernetes configures this store to sync secrets using a Kubernetes cluster provider"
										properties: {
											auth: {
												description:   "Auth configures how secret-manager authenticates with a Kubernetes instance."
												maxProperties: 1
												minProperties: 1
												properties: {
													cert: {
														description: "has both clientCert and clientKey as secretKeySelector"
														properties: {
															clientCert: {
																description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															clientKey: {
																description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														type: "object"
													}
													serviceAccount: {
														description: "points to a service account that should be used for authentication"
														properties: {
															audiences: {
																description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
																items: type: "string"
																type: "array"
															}
															name: {
																description: "The name of the ServiceAccount resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														required: ["name"]
														type: "object"
													}
													token: {
														description: "use static token to authenticate with"
														properties: bearerToken: {
															description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
															properties: {
																key: {
																	description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																	type:        "string"
																}
																name: {
																	description: "The name of the Secret resource being referred to."
																	type:        "string"
																}
																namespace: {
																	description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																	type:        "string"
																}
															}
															type: "object"
														}
														type: "object"
													}
												}
												type: "object"
											}
											remoteNamespace: {
												default:     "default"
												description: "Remote namespace to fetch the secrets from"
												type:        "string"
											}
											server: {
												description: "configures the Kubernetes server Address."
												properties: {
													caBundle: {
														description: "CABundle is a base64-encoded CA certificate"
														format:      "byte"
														type:        "string"
													}
													caProvider: {
														description: "see: https://external-secrets.io/v0.4.1/spec/#external-secrets.io/v1alpha1.CAProvider"
														properties: {
															key: {
																description: "The key where the CA certificate can be found in the Secret or ConfigMap."
																type:        "string"
															}
															name: {
																description: "The name of the object located at the provider type."
																type:        "string"
															}
															namespace: {
																description: "The namespace the Provider type is in. Can only be defined when used in a ClusterSecretStore."
																type:        "string"
															}
															type: {
																description: "The type of provider to use such as \"Secret\", or \"ConfigMap\"."
																enum: [
																	"Secret",
																	"ConfigMap",
																]
																type: "string"
															}
														}
														required: [
															"name",
															"type",
														]
														type: "object"
													}
													url: {
														default:     "kubernetes.default"
														description: "configures the Kubernetes server Address."
														type:        "string"
													}
												}
												type: "object"
											}
										}
										required: ["auth"]
										type: "object"
									}
									onepassword: {
										description: "OnePassword configures this store to sync secrets using the 1Password Cloud provider"
										properties: {
											auth: {
												description: "Auth defines the information necessary to authenticate against OnePassword Connect Server"
												properties: secretRef: {
													description: "OnePasswordAuthSecretRef holds secret references for 1Password credentials."
													properties: connectTokenSecretRef: {
														description: "The ConnectToken is used for authentication to a 1Password Connect Server."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
													required: ["connectTokenSecretRef"]
													type: "object"
												}
												required: ["secretRef"]
												type: "object"
											}
											connectHost: {
												description: "ConnectHost defines the OnePassword Connect Server to connect to"
												type:        "string"
											}
											vaults: {
												additionalProperties: type: "integer"
												description: "Vaults defines which OnePassword vaults to search in which order"
												type:        "object"
											}
										}
										required: [
											"auth",
											"connectHost",
											"vaults",
										]
										type: "object"
									}
									oracle: {
										description: "Oracle configures this store to sync secrets using Oracle Vault provider"
										properties: {
											auth: {
												description: "Auth configures how secret-manager authenticates with the Oracle Vault. If empty, use the instance principal, otherwise the user credentials specified in Auth."
												properties: {
													secretRef: {
														description: "SecretRef to pass through sensitive information."
														properties: {
															fingerprint: {
																description: "Fingerprint is the fingerprint of the API private key."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															privatekey: {
																description: "PrivateKey is the user's API Signing Key in PEM format, used for authentication."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														required: [
															"fingerprint",
															"privatekey",
														]
														type: "object"
													}
													tenancy: {
														description: "Tenancy is the tenancy OCID where user is located."
														type:        "string"
													}
													user: {
														description: "User is an access OCID specific to the account."
														type:        "string"
													}
												}
												required: [
													"secretRef",
													"tenancy",
													"user",
												]
												type: "object"
											}
											region: {
												description: "Region is the region where vault is located."
												type:        "string"
											}
											vault: {
												description: "Vault is the vault's OCID of the specific vault where secret is located."
												type:        "string"
											}
										}
										required: [
											"region",
											"vault",
										]
										type: "object"
									}
									scaleway: {
										description: "Scaleway"
										properties: {
											accessKey: {
												description: "AccessKey is the non-secret part of the api key."
												properties: {
													secretRef: {
														description: "SecretRef references a key in a secret that will be used as value."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
													value: {
														description: "Value can be specified directly to set a value without using a secret."
														type:        "string"
													}
												}
												type: "object"
											}
											apiUrl: {
												description: "APIURL is the url of the api to use. Defaults to https://api.scaleway.com"
												type:        "string"
											}
											projectId: {
												description: "ProjectID is the id of your project, which you can find in the console: https://console.scaleway.com/project/settings"
												type:        "string"
											}
											region: {
												description: "Region where your secrets are located: https://developers.scaleway.com/en/quickstart/#region-and-zone"
												type:        "string"
											}
											secretKey: {
												description: "SecretKey is the non-secret part of the api key."
												properties: {
													secretRef: {
														description: "SecretRef references a key in a secret that will be used as value."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
													value: {
														description: "Value can be specified directly to set a value without using a secret."
														type:        "string"
													}
												}
												type: "object"
											}
										}
										required: [
											"accessKey",
											"projectId",
											"region",
											"secretKey",
										]
										type: "object"
									}
									senhasegura: {
										description: "Senhasegura configures this store to sync secrets using senhasegura provider"
										properties: {
											auth: {
												description: "Auth defines parameters to authenticate in senhasegura"
												properties: {
													clientId: type: "string"
													clientSecretSecretRef: {
														description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
												}
												required: [
													"clientId",
													"clientSecretSecretRef",
												]
												type: "object"
											}
											ignoreSslCertificate: {
												default:     false
												description: "IgnoreSslCertificate defines if SSL certificate must be ignored"
												type:        "boolean"
											}
											module: {
												description: "Module defines which senhasegura module should be used to get secrets"
												type:        "string"
											}
											url: {
												description: "URL of senhasegura"
												type:        "string"
											}
										}
										required: [
											"auth",
											"module",
											"url",
										]
										type: "object"
									}
									vault: {
										description: "Vault configures this store to sync secrets using Hashi provider"
										properties: {
											auth: {
												description: "Auth configures how secret-manager authenticates with the Vault server."
												properties: {
													appRole: {
														description: "AppRole authenticates with Vault using the App Role auth mechanism, with the role and secret stored in a Kubernetes Secret resource."
														properties: {
															path: {
																default:     "approle"
																description: "Path where the App Role authentication backend is mounted in Vault, e.g: \"approle\""
																type:        "string"
															}
															roleId: {
																description: "RoleID configured in the App Role authentication backend when setting up the authentication backend in Vault."
																type:        "string"
															}
															roleRef: {
																description: "Reference to a key in a Secret that contains the App Role ID used to authenticate with Vault. The `key` field must be specified and denotes which entry within the Secret resource is used as the app role id."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															secretRef: {
																description: "Reference to a key in a Secret that contains the App Role secret used to authenticate with Vault. The `key` field must be specified and denotes which entry within the Secret resource is used as the app role secret."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														required: [
															"path",
															"secretRef",
														]
														type: "object"
													}
													cert: {
														description: "Cert authenticates with TLS Certificates by passing client certificate, private key and ca certificate Cert authentication method"
														properties: {
															clientCert: {
																description: "ClientCert is a certificate to authenticate using the Cert Vault authentication method"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															secretRef: {
																description: "SecretRef to a key in a Secret resource containing client private key to authenticate with Vault using the Cert authentication method"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														type: "object"
													}
													iam: {
														description: "Iam authenticates with vault by passing a special AWS request signed with AWS IAM credentials AWS IAM authentication method"
														properties: {
															externalID: {
																description: "AWS External ID set on assumed IAM roles"
																type:        "string"
															}
															jwt: {
																description: "Specify a service account with IRSA enabled"
																properties: serviceAccountRef: {
																	description: "A reference to a ServiceAccount resource."
																	properties: {
																		audiences: {
																			description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
																			items: type: "string"
																			type: "array"
																		}
																		name: {
																			description: "The name of the ServiceAccount resource being referred to."
																			type:        "string"
																		}
																		namespace: {
																			description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																			type:        "string"
																		}
																	}
																	required: ["name"]
																	type: "object"
																}
																type: "object"
															}
															path: {
																description: "Path where the AWS auth method is enabled in Vault, e.g: \"aws\""
																type:        "string"
															}
															region: {
																description: "AWS region"
																type:        "string"
															}
															role: {
																description: "This is the AWS role to be assumed before talking to vault"
																type:        "string"
															}
															secretRef: {
																description: "Specify credentials in a Secret object"
																properties: {
																	accessKeyIDSecretRef: {
																		description: "The AccessKeyID is used for authentication"
																		properties: {
																			key: {
																				description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																				type:        "string"
																			}
																			name: {
																				description: "The name of the Secret resource being referred to."
																				type:        "string"
																			}
																			namespace: {
																				description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																				type:        "string"
																			}
																		}
																		type: "object"
																	}
																	secretAccessKeySecretRef: {
																		description: "The SecretAccessKey is used for authentication"
																		properties: {
																			key: {
																				description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																				type:        "string"
																			}
																			name: {
																				description: "The name of the Secret resource being referred to."
																				type:        "string"
																			}
																			namespace: {
																				description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																				type:        "string"
																			}
																		}
																		type: "object"
																	}
																	sessionTokenSecretRef: {
																		description: "The SessionToken used for authentication This must be defined if AccessKeyID and SecretAccessKey are temporary credentials see: https://docs.aws.amazon.com/IAM/latest/UserGuide/id_credentials_temp_use-resources.html"
																		properties: {
																			key: {
																				description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																				type:        "string"
																			}
																			name: {
																				description: "The name of the Secret resource being referred to."
																				type:        "string"
																			}
																			namespace: {
																				description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																				type:        "string"
																			}
																		}
																		type: "object"
																	}
																}
																type: "object"
															}
															vaultAwsIamServerID: {
																description: "X-Vault-AWS-IAM-Server-ID is an additional header used by Vault IAM auth method to mitigate against different types of replay attacks. More details here: https://developer.hashicorp.com/vault/docs/auth/aws"
																type:        "string"
															}
															vaultRole: {
																description: "Vault Role. In vault, a role describes an identity with a set of permissions, groups, or policies you want to attach a user of the secrets engine"
																type:        "string"
															}
														}
														required: ["vaultRole"]
														type: "object"
													}
													jwt: {
														description: "Jwt authenticates with Vault by passing role and JWT token using the JWT/OIDC authentication method"
														properties: {
															kubernetesServiceAccountToken: {
																description: "Optional ServiceAccountToken specifies the Kubernetes service account for which to request a token for with the `TokenRequest` API."
																properties: {
																	audiences: {
																		description: "Optional audiences field that will be used to request a temporary Kubernetes service account token for the service account referenced by `serviceAccountRef`. Defaults to a single audience `vault` it not specified. Deprecated: use serviceAccountRef.Audiences instead"
																		items: type: "string"
																		type: "array"
																	}
																	expirationSeconds: {
																		description: "Optional expiration time in seconds that will be used to request a temporary Kubernetes service account token for the service account referenced by `serviceAccountRef`. Deprecated: this will be removed in the future. Defaults to 10 minutes."
																		format:      "int64"
																		type:        "integer"
																	}
																	serviceAccountRef: {
																		description: "Service account field containing the name of a kubernetes ServiceAccount."
																		properties: {
																			audiences: {
																				description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
																				items: type: "string"
																				type: "array"
																			}
																			name: {
																				description: "The name of the ServiceAccount resource being referred to."
																				type:        "string"
																			}
																			namespace: {
																				description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																				type:        "string"
																			}
																		}
																		required: ["name"]
																		type: "object"
																	}
																}
																required: ["serviceAccountRef"]
																type: "object"
															}
															path: {
																default:     "jwt"
																description: "Path where the JWT authentication backend is mounted in Vault, e.g: \"jwt\""
																type:        "string"
															}
															role: {
																description: "Role is a JWT role to authenticate using the JWT/OIDC Vault authentication method"
																type:        "string"
															}
															secretRef: {
																description: "Optional SecretRef that refers to a key in a Secret resource containing JWT token to authenticate with Vault using the JWT/OIDC authentication method."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														required: ["path"]
														type: "object"
													}
													kubernetes: {
														description: "Kubernetes authenticates with Vault by passing the ServiceAccount token stored in the named Secret resource to the Vault server."
														properties: {
															mountPath: {
																default:     "kubernetes"
																description: "Path where the Kubernetes authentication backend is mounted in Vault, e.g: \"kubernetes\""
																type:        "string"
															}
															role: {
																description: "A required field containing the Vault Role to assume. A Role binds a Kubernetes ServiceAccount with a set of Vault policies."
																type:        "string"
															}
															secretRef: {
																description: "Optional secret field containing a Kubernetes ServiceAccount JWT used for authenticating with Vault. If a name is specified without a key, `token` is the default. If one is not specified, the one bound to the controller will be used."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															serviceAccountRef: {
																description: "Optional service account field containing the name of a kubernetes ServiceAccount. If the service account is specified, the service account secret token JWT will be used for authenticating with Vault. If the service account selector is not supplied, the secretRef will be used instead."
																properties: {
																	audiences: {
																		description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
																		items: type: "string"
																		type: "array"
																	}
																	name: {
																		description: "The name of the ServiceAccount resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																required: ["name"]
																type: "object"
															}
														}
														required: [
															"mountPath",
															"role",
														]
														type: "object"
													}
													ldap: {
														description: "Ldap authenticates with Vault by passing username/password pair using the LDAP authentication method"
														properties: {
															path: {
																default:     "ldap"
																description: "Path where the LDAP authentication backend is mounted in Vault, e.g: \"ldap\""
																type:        "string"
															}
															secretRef: {
																description: "SecretRef to a key in a Secret resource containing password for the LDAP user used to authenticate with Vault using the LDAP authentication method"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															username: {
																description: "Username is a LDAP user name used to authenticate using the LDAP Vault authentication method"
																type:        "string"
															}
														}
														required: [
															"path",
															"username",
														]
														type: "object"
													}
													tokenSecretRef: {
														description: "TokenSecretRef authenticates with Vault by presenting a token."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
													userPass: {
														description: "UserPass authenticates with Vault by passing username/password pair"
														properties: {
															path: {
																default:     "user"
																description: "Path where the UserPassword authentication backend is mounted in Vault, e.g: \"user\""
																type:        "string"
															}
															secretRef: {
																description: "SecretRef to a key in a Secret resource containing password for the user used to authenticate with Vault using the UserPass authentication method"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															username: {
																description: "Username is a user name used to authenticate using the UserPass Vault authentication method"
																type:        "string"
															}
														}
														required: [
															"path",
															"username",
														]
														type: "object"
													}
												}
												type: "object"
											}
											caBundle: {
												description: "PEM encoded CA bundle used to validate Vault server certificate. Only used if the Server URL is using HTTPS protocol. This parameter is ignored for plain HTTP protocol connection. If not set the system root certificates are used to validate the TLS connection."
												format:      "byte"
												type:        "string"
											}
											caProvider: {
												description: "The provider for the CA bundle to use to validate Vault server certificate."
												properties: {
													key: {
														description: "The key where the CA certificate can be found in the Secret or ConfigMap."
														type:        "string"
													}
													name: {
														description: "The name of the object located at the provider type."
														type:        "string"
													}
													namespace: {
														description: "The namespace the Provider type is in. Can only be defined when used in a ClusterSecretStore."
														type:        "string"
													}
													type: {
														description: "The type of provider to use such as \"Secret\", or \"ConfigMap\"."
														enum: [
															"Secret",
															"ConfigMap",
														]
														type: "string"
													}
												}
												required: [
													"name",
													"type",
												]
												type: "object"
											}
											forwardInconsistent: {
												description: "ForwardInconsistent tells Vault to forward read-after-write requests to the Vault leader instead of simply retrying within a loop. This can increase performance if the option is enabled serverside. https://www.vaultproject.io/docs/configuration/replication#allow_forwarding_via_header"
												type:        "boolean"
											}
											namespace: {
												description: "Name of the vault namespace. Namespaces is a set of features within Vault Enterprise that allows Vault environments to support Secure Multi-tenancy. e.g: \"ns1\". More about namespaces can be found here https://www.vaultproject.io/docs/enterprise/namespaces"
												type:        "string"
											}
											path: {
												description: "Path is the mount path of the Vault KV backend endpoint, e.g: \"secret\". The v2 KV secret engine version specific \"/data\" path suffix for fetching secrets from Vault is optional and will be appended if not present in specified path."
												type:        "string"
											}
											readYourWrites: {
												description: "ReadYourWrites ensures isolated read-after-write semantics by providing discovered cluster replication states in each request. More information about eventual consistency in Vault can be found here https://www.vaultproject.io/docs/enterprise/consistency"
												type:        "boolean"
											}
											server: {
												description: "Server is the connection address for the Vault server, e.g: \"https://vault.example.com:8200\"."
												type:        "string"
											}
											version: {
												default:     "v2"
												description: "Version is the Vault KV secret engine version. This can be either \"v1\" or \"v2\". Version defaults to \"v2\"."
												enum: [
													"v1",
													"v2",
												]
												type: "string"
											}
										}
										required: [
											"auth",
											"server",
										]
										type: "object"
									}
									webhook: {
										description: "Webhook configures this store to sync secrets using a generic templated webhook"
										properties: {
											body: {
												description: "Body"
												type:        "string"
											}
											caBundle: {
												description: "PEM encoded CA bundle used to validate webhook server certificate. Only used if the Server URL is using HTTPS protocol. This parameter is ignored for plain HTTP protocol connection. If not set the system root certificates are used to validate the TLS connection."
												format:      "byte"
												type:        "string"
											}
											caProvider: {
												description: "The provider for the CA bundle to use to validate webhook server certificate."
												properties: {
													key: {
														description: "The key the value inside of the provider type to use, only used with \"Secret\" type"
														type:        "string"
													}
													name: {
														description: "The name of the object located at the provider type."
														type:        "string"
													}
													namespace: {
														description: "The namespace the Provider type is in."
														type:        "string"
													}
													type: {
														description: "The type of provider to use such as \"Secret\", or \"ConfigMap\"."
														enum: [
															"Secret",
															"ConfigMap",
														]
														type: "string"
													}
												}
												required: [
													"name",
													"type",
												]
												type: "object"
											}
											headers: {
												additionalProperties: type: "string"
												description: "Headers"
												type:        "object"
											}
											method: {
												description: "Webhook Method"
												type:        "string"
											}
											result: {
												description: "Result formatting"
												properties: jsonPath: {
													description: "Json path of return value"
													type:        "string"
												}
												type: "object"
											}
											secrets: {
												description: "Secrets to fill in templates These secrets will be passed to the templating function as key value pairs under the given name"
												items: {
													properties: {
														name: {
															description: "Name of this secret in templates"
															type:        "string"
														}
														secretRef: {
															description: "Secret ref to fill in credentials"
															properties: {
																key: {
																	description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																	type:        "string"
																}
																name: {
																	description: "The name of the Secret resource being referred to."
																	type:        "string"
																}
																namespace: {
																	description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																	type:        "string"
																}
															}
															type: "object"
														}
													}
													required: [
														"name",
														"secretRef",
													]
													type: "object"
												}
												type: "array"
											}
											timeout: {
												description: "Timeout"
												type:        "string"
											}
											url: {
												description: "Webhook url to call"
												type:        "string"
											}
										}
										required: [
											"result",
											"url",
										]
										type: "object"
									}
									yandexcertificatemanager: {
										description: "YandexCertificateManager configures this store to sync secrets using Yandex Certificate Manager provider"
										properties: {
											apiEndpoint: {
												description: "Yandex.Cloud API endpoint (e.g. 'api.cloud.yandex.net:443')"
												type:        "string"
											}
											auth: {
												description: "Auth defines the information necessary to authenticate against Yandex Certificate Manager"
												properties: authorizedKeySecretRef: {
													description: "The authorized key used for authentication"
													properties: {
														key: {
															description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
															type:        "string"
														}
														name: {
															description: "The name of the Secret resource being referred to."
															type:        "string"
														}
														namespace: {
															description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
															type:        "string"
														}
													}
													type: "object"
												}
												type: "object"
											}
											caProvider: {
												description: "The provider for the CA bundle to use to validate Yandex.Cloud server certificate."
												properties: certSecretRef: {
													description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
													properties: {
														key: {
															description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
															type:        "string"
														}
														name: {
															description: "The name of the Secret resource being referred to."
															type:        "string"
														}
														namespace: {
															description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
															type:        "string"
														}
													}
													type: "object"
												}
												type: "object"
											}
										}
										required: ["auth"]
										type: "object"
									}
									yandexlockbox: {
										description: "YandexLockbox configures this store to sync secrets using Yandex Lockbox provider"
										properties: {
											apiEndpoint: {
												description: "Yandex.Cloud API endpoint (e.g. 'api.cloud.yandex.net:443')"
												type:        "string"
											}
											auth: {
												description: "Auth defines the information necessary to authenticate against Yandex Lockbox"
												properties: authorizedKeySecretRef: {
													description: "The authorized key used for authentication"
													properties: {
														key: {
															description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
															type:        "string"
														}
														name: {
															description: "The name of the Secret resource being referred to."
															type:        "string"
														}
														namespace: {
															description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
															type:        "string"
														}
													}
													type: "object"
												}
												type: "object"
											}
											caProvider: {
												description: "The provider for the CA bundle to use to validate Yandex.Cloud server certificate."
												properties: certSecretRef: {
													description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
													properties: {
														key: {
															description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
															type:        "string"
														}
														name: {
															description: "The name of the Secret resource being referred to."
															type:        "string"
														}
														namespace: {
															description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
															type:        "string"
														}
													}
													type: "object"
												}
												type: "object"
											}
										}
										required: ["auth"]
										type: "object"
									}
								}
								type: "object"
							}
							refreshInterval: {
								description: "Used to configure store refresh interval in seconds. Empty or 0 will default to the controller config."
								type:        "integer"
							}
							retrySettings: {
								description: "Used to configure http retries if failed"
								properties: {
									maxRetries: {
										format: "int32"
										type:   "integer"
									}
									retryInterval: type: "string"
								}
								type: "object"
							}
						}
						required: ["provider"]
						type: "object"
					}
					status: {
						description: "SecretStoreStatus defines the observed state of the SecretStore."
						properties: {
							capabilities: {
								description: "SecretStoreCapabilities defines the possible operations a SecretStore can do."
								type:        "string"
							}
							conditions: {
								items: {
									properties: {
										lastTransitionTime: {
											format: "date-time"
											type:   "string"
										}
										message: type: "string"
										reason: type:  "string"
										status: type:  "string"
										type: type:    "string"
									}
									required: [
										"status",
										"type",
									]
									type: "object"
								}
								type: "array"
							}
						}
						type: "object"
					}
				}
				type: "object"
			}
			served:  true
			storage: true
			subresources: status: {}
		}]
		conversion: {
			strategy: "Webhook"
			webhook: {
				conversionReviewVersions: ["v1"]
				clientConfig: service: {
					name:      "external-secrets-webhook"
					namespace: "external-secrets"
					path:      "/convert"
				}
			}
		}
	}
}, {
	spec: {
		group: "generators.external-secrets.io"
		names: {
			categories: ["ecrauthorizationtoken"]
			kind:     "ECRAuthorizationToken"
			listKind: "ECRAuthorizationTokenList"
			plural:   "ecrauthorizationtokens"
			shortNames: ["ecrauthorizationtoken"]
			singular: "ecrauthorizationtoken"
		}
		scope: "Namespaced"
		versions: [{
			name: "v1alpha1"
			schema: openAPIV3Schema: {
				description: "ECRAuthorizationTokenSpec uses the GetAuthorizationToken API to retrieve an authorization token. The authorization token is valid for 12 hours. The authorizationToken returned is a base64 encoded string that can be decoded and used in a docker login command to authenticate to a registry. For more information, see Registry authentication (https://docs.aws.amazon.com/AmazonECR/latest/userguide/Registries.html#registry_auth) in the Amazon Elastic Container Registry User Guide."
				properties: {
					apiVersion: {
						description: "APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources"
						type:        "string"
					}
					kind: {
						description: "Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds"
						type:        "string"
					}
					metadata: type: "object"
					spec: {
						properties: {
							auth: {
								description: "Auth defines how to authenticate with AWS"
								properties: {
									jwt: {
										description: "Authenticate against AWS using service account tokens."
										properties: serviceAccountRef: {
											description: "A reference to a ServiceAccount resource."
											properties: {
												audiences: {
													description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
													items: type: "string"
													type: "array"
												}
												name: {
													description: "The name of the ServiceAccount resource being referred to."
													type:        "string"
												}
												namespace: {
													description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
													type:        "string"
												}
											}
											required: ["name"]
											type: "object"
										}
										type: "object"
									}
									secretRef: {
										description: "AWSAuthSecretRef holds secret references for AWS credentials both AccessKeyID and SecretAccessKey must be defined in order to properly authenticate."
										properties: {
											accessKeyIDSecretRef: {
												description: "The AccessKeyID is used for authentication"
												properties: {
													key: {
														description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
														type:        "string"
													}
													name: {
														description: "The name of the Secret resource being referred to."
														type:        "string"
													}
													namespace: {
														description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
														type:        "string"
													}
												}
												type: "object"
											}
											secretAccessKeySecretRef: {
												description: "The SecretAccessKey is used for authentication"
												properties: {
													key: {
														description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
														type:        "string"
													}
													name: {
														description: "The name of the Secret resource being referred to."
														type:        "string"
													}
													namespace: {
														description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
														type:        "string"
													}
												}
												type: "object"
											}
											sessionTokenSecretRef: {
												description: "The SessionToken used for authentication This must be defined if AccessKeyID and SecretAccessKey are temporary credentials see: https://docs.aws.amazon.com/IAM/latest/UserGuide/id_credentials_temp_use-resources.html"
												properties: {
													key: {
														description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
														type:        "string"
													}
													name: {
														description: "The name of the Secret resource being referred to."
														type:        "string"
													}
													namespace: {
														description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
														type:        "string"
													}
												}
												type: "object"
											}
										}
										type: "object"
									}
								}
								type: "object"
							}
							region: {
								description: "Region specifies the region to operate in."
								type:        "string"
							}
							role: {
								description: "You can assume a role before making calls to the desired AWS service."
								type:        "string"
							}
						}
						required: ["region"]
						type: "object"
					}
				}
				type: "object"
			}
			served:  true
			storage: true
			subresources: status: {}
		}]
		conversion: {
			strategy: "Webhook"
			webhook: {
				conversionReviewVersions: ["v1"]
				clientConfig: service: {
					name:      "external-secrets-webhook"
					namespace: "external-secrets"
					path:      "/convert"
				}
			}
		}
	}
}, {
	spec: {
		group: "external-secrets.io"
		names: {
			categories: ["externalsecrets"]
			kind:     "ExternalSecret"
			listKind: "ExternalSecretList"
			plural:   "externalsecrets"
			shortNames: ["es"]
			singular: "externalsecret"
		}
		scope: "Namespaced"
		versions: [{
			additionalPrinterColumns: [{
				jsonPath: ".spec.secretStoreRef.name"
				name:     "Store"
				type:     "string"
			}, {
				jsonPath: ".spec.refreshInterval"
				name:     "Refresh Interval"
				type:     "string"
			}, {
				jsonPath: ".status.conditions[?(@.type==\"Ready\")].reason"
				name:     "Status"
				type:     "string"
			}]
			deprecated: true
			name:       "v1alpha1"
			schema: openAPIV3Schema: {
				description: "ExternalSecret is the Schema for the external-secrets API."
				properties: {
					apiVersion: {
						description: "APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources"
						type:        "string"
					}
					kind: {
						description: "Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds"
						type:        "string"
					}
					metadata: type: "object"
					spec: {
						description: "ExternalSecretSpec defines the desired state of ExternalSecret."
						properties: {
							data: {
								description: "Data defines the connection between the Kubernetes Secret keys and the Provider data"
								items: {
									description: "ExternalSecretData defines the connection between the Kubernetes Secret key (spec.data.<key>) and the Provider data."
									properties: {
										remoteRef: {
											description: "ExternalSecretDataRemoteRef defines Provider data location."
											properties: {
												conversionStrategy: {
													default:     "Default"
													description: "Used to define a conversion Strategy"
													type:        "string"
												}
												key: {
													description: "Key is the key used in the Provider, mandatory"
													type:        "string"
												}
												property: {
													description: "Used to select a specific property of the Provider value (if a map), if supported"
													type:        "string"
												}
												version: {
													description: "Used to select a specific version of the Provider value, if supported"
													type:        "string"
												}
											}
											required: ["key"]
											type: "object"
										}
										secretKey: type: "string"
									}
									required: [
										"remoteRef",
										"secretKey",
									]
									type: "object"
								}
								type: "array"
							}
							dataFrom: {
								description: "DataFrom is used to fetch all properties from a specific Provider data If multiple entries are specified, the Secret keys are merged in the specified order"
								items: {
									description: "ExternalSecretDataRemoteRef defines Provider data location."
									properties: {
										conversionStrategy: {
											default:     "Default"
											description: "Used to define a conversion Strategy"
											type:        "string"
										}
										key: {
											description: "Key is the key used in the Provider, mandatory"
											type:        "string"
										}
										property: {
											description: "Used to select a specific property of the Provider value (if a map), if supported"
											type:        "string"
										}
										version: {
											description: "Used to select a specific version of the Provider value, if supported"
											type:        "string"
										}
									}
									required: ["key"]
									type: "object"
								}
								type: "array"
							}
							refreshInterval: {
								default:     "1h"
								description: "RefreshInterval is the amount of time before the values are read again from the SecretStore provider Valid time units are \"ns\", \"us\" (or \"µs\"), \"ms\", \"s\", \"m\", \"h\" May be set to zero to fetch and create it once. Defaults to 1h."
								type:        "string"
							}
							secretStoreRef: {
								description: "SecretStoreRef defines which SecretStore to fetch the ExternalSecret data."
								properties: {
									kind: {
										description: "Kind of the SecretStore resource (SecretStore or ClusterSecretStore) Defaults to `SecretStore`"
										type:        "string"
									}
									name: {
										description: "Name of the SecretStore resource"
										type:        "string"
									}
								}
								required: ["name"]
								type: "object"
							}
							target: {
								description: "ExternalSecretTarget defines the Kubernetes Secret to be created There can be only one target per ExternalSecret."
								properties: {
									creationPolicy: {
										default:     "Owner"
										description: "CreationPolicy defines rules on how to create the resulting Secret Defaults to 'Owner'"
										type:        "string"
									}
									immutable: {
										description: "Immutable defines if the final secret will be immutable"
										type:        "boolean"
									}
									name: {
										description: "Name defines the name of the Secret resource to be managed This field is immutable Defaults to the .metadata.name of the ExternalSecret resource"
										type:        "string"
									}
									template: {
										description: "Template defines a blueprint for the created Secret resource."
										properties: {
											data: {
												additionalProperties: type: "string"
												type: "object"
											}
											engineVersion: {
												default:     "v1"
												description: "EngineVersion specifies the template engine version that should be used to compile/execute the template specified in .data and .templateFrom[]."
												type:        "string"
											}
											metadata: {
												description: "ExternalSecretTemplateMetadata defines metadata fields for the Secret blueprint."
												properties: {
													annotations: {
														additionalProperties: type: "string"
														type: "object"
													}
													labels: {
														additionalProperties: type: "string"
														type: "object"
													}
												}
												type: "object"
											}
											templateFrom: {
												items: {
													maxProperties: 1
													minProperties: 1
													properties: {
														configMap: {
															properties: {
																items: {
																	items: {
																		properties: key: type: "string"
																		required: ["key"]
																		type: "object"
																	}
																	type: "array"
																}
																name: type: "string"
															}
															required: [
																"items",
																"name",
															]
															type: "object"
														}
														secret: {
															properties: {
																items: {
																	items: {
																		properties: key: type: "string"
																		required: ["key"]
																		type: "object"
																	}
																	type: "array"
																}
																name: type: "string"
															}
															required: [
																"items",
																"name",
															]
															type: "object"
														}
													}
													type: "object"
												}
												type: "array"
											}
											type: type: "string"
										}
										type: "object"
									}
								}
								type: "object"
							}
						}
						required: [
							"secretStoreRef",
							"target",
						]
						type: "object"
					}
					status: {
						properties: {
							binding: {
								description: "Binding represents a servicebinding.io Provisioned Service reference to the secret"
								properties: name: {
									description: "Name of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names TODO: Add other useful fields. apiVersion, kind, uid?"
									type:        "string"
								}
								type:                    "object"
								"x-kubernetes-map-type": "atomic"
							}
							conditions: {
								items: {
									properties: {
										lastTransitionTime: {
											format: "date-time"
											type:   "string"
										}
										message: type: "string"
										reason: type:  "string"
										status: type:  "string"
										type: type:    "string"
									}
									required: [
										"status",
										"type",
									]
									type: "object"
								}
								type: "array"
							}
							refreshTime: {
								description: "refreshTime is the time and date the external secret was fetched and the target secret updated"
								format:      "date-time"
								nullable:    true
								type:        "string"
							}
							syncedResourceVersion: {
								description: "SyncedResourceVersion keeps track of the last synced version"
								type:        "string"
							}
						}
						type: "object"
					}
				}
				type: "object"
			}
			served:  true
			storage: false
			subresources: status: {}
		}, {
			additionalPrinterColumns: [{
				jsonPath: ".spec.secretStoreRef.name"
				name:     "Store"
				type:     "string"
			}, {
				jsonPath: ".spec.refreshInterval"
				name:     "Refresh Interval"
				type:     "string"
			}, {
				jsonPath: ".status.conditions[?(@.type==\"Ready\")].reason"
				name:     "Status"
				type:     "string"
			}, {
				jsonPath: ".status.conditions[?(@.type==\"Ready\")].status"
				name:     "Ready"
				type:     "string"
			}]
			name: "v1beta1"
			schema: openAPIV3Schema: {
				description: "ExternalSecret is the Schema for the external-secrets API."
				properties: {
					apiVersion: {
						description: "APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources"
						type:        "string"
					}
					kind: {
						description: "Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds"
						type:        "string"
					}
					metadata: type: "object"
					spec: {
						description: "ExternalSecretSpec defines the desired state of ExternalSecret."
						properties: {
							data: {
								description: "Data defines the connection between the Kubernetes Secret keys and the Provider data"
								items: {
									description: "ExternalSecretData defines the connection between the Kubernetes Secret key (spec.data.<key>) and the Provider data."
									properties: {
										remoteRef: {
											description: "RemoteRef points to the remote secret and defines which secret (version/property/..) to fetch."
											properties: {
												conversionStrategy: {
													default:     "Default"
													description: "Used to define a conversion Strategy"
													type:        "string"
												}
												decodingStrategy: {
													default:     "None"
													description: "Used to define a decoding Strategy"
													type:        "string"
												}
												key: {
													description: "Key is the key used in the Provider, mandatory"
													type:        "string"
												}
												metadataPolicy: {
													description: "Policy for fetching tags/labels from provider secrets, possible options are Fetch, None. Defaults to None"
													type:        "string"
												}
												property: {
													description: "Used to select a specific property of the Provider value (if a map), if supported"
													type:        "string"
												}
												version: {
													description: "Used to select a specific version of the Provider value, if supported"
													type:        "string"
												}
											}
											required: ["key"]
											type: "object"
										}
										secretKey: {
											description: "SecretKey defines the key in which the controller stores the value. This is the key in the Kind=Secret"
											type:        "string"
										}
										sourceRef: {
											description:   "SourceRef allows you to override the source from which the value will pulled from."
											maxProperties: 1
											properties: {
												generatorRef: {
													description: "GeneratorRef points to a generator custom resource in"
													properties: {
														apiVersion: {
															default:     "generators.external-secrets.io/v1alpha1"
															description: "Specify the apiVersion of the generator resource"
															type:        "string"
														}
														kind: {
															description: "Specify the Kind of the resource, e.g. Password, ACRAccessToken etc."
															type:        "string"
														}
														name: {
															description: "Specify the name of the generator resource"
															type:        "string"
														}
													}
													required: [
														"kind",
														"name",
													]
													type: "object"
												}
												storeRef: {
													description: "SecretStoreRef defines which SecretStore to fetch the ExternalSecret data."
													properties: {
														kind: {
															description: "Kind of the SecretStore resource (SecretStore or ClusterSecretStore) Defaults to `SecretStore`"
															type:        "string"
														}
														name: {
															description: "Name of the SecretStore resource"
															type:        "string"
														}
													}
													required: ["name"]
													type: "object"
												}
											}
											type: "object"
										}
									}
									required: [
										"remoteRef",
										"secretKey",
									]
									type: "object"
								}
								type: "array"
							}
							dataFrom: {
								description: "DataFrom is used to fetch all properties from a specific Provider data If multiple entries are specified, the Secret keys are merged in the specified order"
								items: {
									properties: {
										extract: {
											description: "Used to extract multiple key/value pairs from one secret Note: Extract does not support sourceRef.Generator or sourceRef.GeneratorRef."
											properties: {
												conversionStrategy: {
													default:     "Default"
													description: "Used to define a conversion Strategy"
													type:        "string"
												}
												decodingStrategy: {
													default:     "None"
													description: "Used to define a decoding Strategy"
													type:        "string"
												}
												key: {
													description: "Key is the key used in the Provider, mandatory"
													type:        "string"
												}
												metadataPolicy: {
													description: "Policy for fetching tags/labels from provider secrets, possible options are Fetch, None. Defaults to None"
													type:        "string"
												}
												property: {
													description: "Used to select a specific property of the Provider value (if a map), if supported"
													type:        "string"
												}
												version: {
													description: "Used to select a specific version of the Provider value, if supported"
													type:        "string"
												}
											}
											required: ["key"]
											type: "object"
										}
										find: {
											description: "Used to find secrets based on tags or regular expressions Note: Find does not support sourceRef.Generator or sourceRef.GeneratorRef."
											properties: {
												conversionStrategy: {
													default:     "Default"
													description: "Used to define a conversion Strategy"
													type:        "string"
												}
												decodingStrategy: {
													default:     "None"
													description: "Used to define a decoding Strategy"
													type:        "string"
												}
												name: {
													description: "Finds secrets based on the name."
													properties: regexp: {
														description: "Finds secrets base"
														type:        "string"
													}
													type: "object"
												}
												path: {
													description: "A root path to start the find operations."
													type:        "string"
												}
												tags: {
													additionalProperties: type: "string"
													description: "Find secrets based on tags."
													type:        "object"
												}
											}
											type: "object"
										}
										rewrite: {
											description: "Used to rewrite secret Keys after getting them from the secret Provider Multiple Rewrite operations can be provided. They are applied in a layered order (first to last)"
											items: {
												properties: regexp: {
													description: "Used to rewrite with regular expressions. The resulting key will be the output of a regexp.ReplaceAll operation."
													properties: {
														source: {
															description: "Used to define the regular expression of a re.Compiler."
															type:        "string"
														}
														target: {
															description: "Used to define the target pattern of a ReplaceAll operation."
															type:        "string"
														}
													}
													required: [
														"source",
														"target",
													]
													type: "object"
												}
												type: "object"
											}
											type: "array"
										}
										sourceRef: {
											description:   "SourceRef points to a store or generator which contains secret values ready to use. Use this in combination with Extract or Find pull values out of a specific SecretStore. When sourceRef points to a generator Extract or Find is not supported. The generator returns a static map of values"
											maxProperties: 1
											properties: {
												generatorRef: {
													description: "GeneratorRef points to a generator custom resource in"
													properties: {
														apiVersion: {
															default:     "generators.external-secrets.io/v1alpha1"
															description: "Specify the apiVersion of the generator resource"
															type:        "string"
														}
														kind: {
															description: "Specify the Kind of the resource, e.g. Password, ACRAccessToken etc."
															type:        "string"
														}
														name: {
															description: "Specify the name of the generator resource"
															type:        "string"
														}
													}
													required: [
														"kind",
														"name",
													]
													type: "object"
												}
												storeRef: {
													description: "SecretStoreRef defines which SecretStore to fetch the ExternalSecret data."
													properties: {
														kind: {
															description: "Kind of the SecretStore resource (SecretStore or ClusterSecretStore) Defaults to `SecretStore`"
															type:        "string"
														}
														name: {
															description: "Name of the SecretStore resource"
															type:        "string"
														}
													}
													required: ["name"]
													type: "object"
												}
											}
											type: "object"
										}
									}
									type: "object"
								}
								type: "array"
							}
							refreshInterval: {
								default:     "1h"
								description: "RefreshInterval is the amount of time before the values are read again from the SecretStore provider Valid time units are \"ns\", \"us\" (or \"µs\"), \"ms\", \"s\", \"m\", \"h\" May be set to zero to fetch and create it once. Defaults to 1h."
								type:        "string"
							}
							secretStoreRef: {
								description: "SecretStoreRef defines which SecretStore to fetch the ExternalSecret data."
								properties: {
									kind: {
										description: "Kind of the SecretStore resource (SecretStore or ClusterSecretStore) Defaults to `SecretStore`"
										type:        "string"
									}
									name: {
										description: "Name of the SecretStore resource"
										type:        "string"
									}
								}
								required: ["name"]
								type: "object"
							}
							target: {
								default: {
									creationPolicy: "Owner"
									deletionPolicy: "Retain"
								}
								description: "ExternalSecretTarget defines the Kubernetes Secret to be created There can be only one target per ExternalSecret."
								properties: {
									creationPolicy: {
										default:     "Owner"
										description: "CreationPolicy defines rules on how to create the resulting Secret Defaults to 'Owner'"
										enum: [
											"Owner",
											"Orphan",
											"Merge",
											"None",
										]
										type: "string"
									}
									deletionPolicy: {
										default:     "Retain"
										description: "DeletionPolicy defines rules on how to delete the resulting Secret Defaults to 'Retain'"
										enum: [
											"Delete",
											"Merge",
											"Retain",
										]
										type: "string"
									}
									immutable: {
										description: "Immutable defines if the final secret will be immutable"
										type:        "boolean"
									}
									name: {
										description: "Name defines the name of the Secret resource to be managed This field is immutable Defaults to the .metadata.name of the ExternalSecret resource"
										type:        "string"
									}
									template: {
										description: "Template defines a blueprint for the created Secret resource."
										properties: {
											data: {
												additionalProperties: type: "string"
												type: "object"
											}
											engineVersion: {
												default: "v2"
												type:    "string"
											}
											mergePolicy: {
												default: "Replace"
												type:    "string"
											}
											metadata: {
												description: "ExternalSecretTemplateMetadata defines metadata fields for the Secret blueprint."
												properties: {
													annotations: {
														additionalProperties: type: "string"
														type: "object"
													}
													labels: {
														additionalProperties: type: "string"
														type: "object"
													}
												}
												type: "object"
											}
											templateFrom: {
												items: {
													properties: {
														configMap: {
															properties: {
																items: {
																	items: {
																		properties: {
																			key: type: "string"
																			templateAs: {
																				default: "Values"
																				type:    "string"
																			}
																		}
																		required: ["key"]
																		type: "object"
																	}
																	type: "array"
																}
																name: type: "string"
															}
															required: [
																"items",
																"name",
															]
															type: "object"
														}
														literal: type: "string"
														secret: {
															properties: {
																items: {
																	items: {
																		properties: {
																			key: type: "string"
																			templateAs: {
																				default: "Values"
																				type:    "string"
																			}
																		}
																		required: ["key"]
																		type: "object"
																	}
																	type: "array"
																}
																name: type: "string"
															}
															required: [
																"items",
																"name",
															]
															type: "object"
														}
														target: {
															default: "Data"
															type:    "string"
														}
													}
													type: "object"
												}
												type: "array"
											}
											type: type: "string"
										}
										type: "object"
									}
								}
								type: "object"
							}
						}
						type: "object"
					}
					status: {
						properties: {
							binding: {
								description: "Binding represents a servicebinding.io Provisioned Service reference to the secret"
								properties: name: {
									description: "Name of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names TODO: Add other useful fields. apiVersion, kind, uid?"
									type:        "string"
								}
								type:                    "object"
								"x-kubernetes-map-type": "atomic"
							}
							conditions: {
								items: {
									properties: {
										lastTransitionTime: {
											format: "date-time"
											type:   "string"
										}
										message: type: "string"
										reason: type:  "string"
										status: type:  "string"
										type: type:    "string"
									}
									required: [
										"status",
										"type",
									]
									type: "object"
								}
								type: "array"
							}
							refreshTime: {
								description: "refreshTime is the time and date the external secret was fetched and the target secret updated"
								format:      "date-time"
								nullable:    true
								type:        "string"
							}
							syncedResourceVersion: {
								description: "SyncedResourceVersion keeps track of the last synced version"
								type:        "string"
							}
						}
						type: "object"
					}
				}
				type: "object"
			}
			served:  true
			storage: true
			subresources: status: {}
		}]
		conversion: {
			strategy: "Webhook"
			webhook: {
				conversionReviewVersions: ["v1"]
				clientConfig: service: {
					name:      "external-secrets-webhook"
					namespace: "external-secrets"
					path:      "/convert"
				}
			}
		}
	}
}, {
	spec: {
		group: "generators.external-secrets.io"
		names: {
			categories: ["fake"]
			kind:     "Fake"
			listKind: "FakeList"
			plural:   "fakes"
			shortNames: ["fake"]
			singular: "fake"
		}
		scope: "Namespaced"
		versions: [{
			name: "v1alpha1"
			schema: openAPIV3Schema: {
				description: "Fake generator is used for testing. It lets you define a static set of credentials that is always returned."
				properties: {
					apiVersion: {
						description: "APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources"
						type:        "string"
					}
					kind: {
						description: "Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds"
						type:        "string"
					}
					metadata: type: "object"
					spec: {
						description: "FakeSpec contains the static data."
						properties: {
							controller: {
								description: "Used to select the correct ESO controller (think: ingress.ingressClassName) The ESO controller is instantiated with a specific controller name and filters VDS based on this property"
								type:        "string"
							}
							data: {
								additionalProperties: type: "string"
								description: "Data defines the static data returned by this generator."
								type:        "object"
							}
						}
						type: "object"
					}
				}
				type: "object"
			}
			served:  true
			storage: true
			subresources: status: {}
		}]
		conversion: {
			strategy: "Webhook"
			webhook: {
				conversionReviewVersions: ["v1"]
				clientConfig: service: {
					name:      "external-secrets-webhook"
					namespace: "external-secrets"
					path:      "/convert"
				}
			}
		}
	}
}, {
	spec: {
		group: "generators.external-secrets.io"
		names: {
			categories: ["gcraccesstoken"]
			kind:     "GCRAccessToken"
			listKind: "GCRAccessTokenList"
			plural:   "gcraccesstokens"
			shortNames: ["gcraccesstoken"]
			singular: "gcraccesstoken"
		}
		scope: "Namespaced"
		versions: [{
			name: "v1alpha1"
			schema: openAPIV3Schema: {
				description: "GCRAccessToken generates an GCP access token that can be used to authenticate with GCR."
				properties: {
					apiVersion: {
						description: "APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources"
						type:        "string"
					}
					kind: {
						description: "Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds"
						type:        "string"
					}
					metadata: type: "object"
					spec: {
						properties: {
							auth: {
								description: "Auth defines the means for authenticating with GCP"
								properties: {
									secretRef: {
										properties: secretAccessKeySecretRef: {
											description: "The SecretAccessKey is used for authentication"
											properties: {
												key: {
													description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
													type:        "string"
												}
												name: {
													description: "The name of the Secret resource being referred to."
													type:        "string"
												}
												namespace: {
													description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
													type:        "string"
												}
											}
											type: "object"
										}
										type: "object"
									}
									workloadIdentity: {
										properties: {
											clusterLocation: type:  "string"
											clusterName: type:      "string"
											clusterProjectID: type: "string"
											serviceAccountRef: {
												description: "A reference to a ServiceAccount resource."
												properties: {
													audiences: {
														description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
														items: type: "string"
														type: "array"
													}
													name: {
														description: "The name of the ServiceAccount resource being referred to."
														type:        "string"
													}
													namespace: {
														description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
														type:        "string"
													}
												}
												required: ["name"]
												type: "object"
											}
										}
										required: [
											"clusterLocation",
											"clusterName",
											"serviceAccountRef",
										]
										type: "object"
									}
								}
								type: "object"
							}
							projectID: {
								description: "ProjectID defines which project to use to authenticate with"
								type:        "string"
							}
						}
						required: [
							"auth",
							"projectID",
						]
						type: "object"
					}
				}
				type: "object"
			}
			served:  true
			storage: true
			subresources: status: {}
		}]
		conversion: {
			strategy: "Webhook"
			webhook: {
				conversionReviewVersions: ["v1"]
				clientConfig: service: {
					name:      "external-secrets-webhook"
					namespace: "external-secrets"
					path:      "/convert"
				}
			}
		}
	}
}, {
	spec: {
		group: "generators.external-secrets.io"
		names: {
			categories: ["password"]
			kind:     "Password"
			listKind: "PasswordList"
			plural:   "passwords"
			shortNames: ["password"]
			singular: "password"
		}
		scope: "Namespaced"
		versions: [{
			name: "v1alpha1"
			schema: openAPIV3Schema: {
				description: "Password generates a random password based on the configuration parameters in spec. You can specify the length, characterset and other attributes."
				properties: {
					apiVersion: {
						description: "APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources"
						type:        "string"
					}
					kind: {
						description: "Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds"
						type:        "string"
					}
					metadata: type: "object"
					spec: {
						description: "PasswordSpec controls the behavior of the password generator."
						properties: {
							allowRepeat: {
								default:     false
								description: "set AllowRepeat to true to allow repeating characters."
								type:        "boolean"
							}
							digits: {
								description: "Digits specifies the number of digits in the generated password. If omitted it defaults to 25% of the length of the password"
								type:        "integer"
							}
							length: {
								default:     24
								description: "Length of the password to be generated. Defaults to 24"
								type:        "integer"
							}
							noUpper: {
								default:     false
								description: "Set NoUpper to disable uppercase characters"
								type:        "boolean"
							}
							symbolCharacters: {
								description: "SymbolCharacters specifies the special characters that should be used in the generated password."
								type:        "string"
							}
							symbols: {
								description: "Symbols specifies the number of symbol characters in the generated password. If omitted it defaults to 25% of the length of the password"
								type:        "integer"
							}
						}
						required: [
							"allowRepeat",
							"length",
							"noUpper",
						]
						type: "object"
					}
				}
				type: "object"
			}
			served:  true
			storage: true
			subresources: status: {}
		}]
		conversion: {
			strategy: "Webhook"
			webhook: {
				conversionReviewVersions: ["v1"]
				clientConfig: service: {
					name:      "external-secrets-webhook"
					namespace: "external-secrets"
					path:      "/convert"
				}
			}
		}
	}
}, {
	spec: {
		group: "external-secrets.io"
		names: {
			categories: ["pushsecrets"]
			kind:     "PushSecret"
			listKind: "PushSecretList"
			plural:   "pushsecrets"
			singular: "pushsecret"
		}
		scope: "Namespaced"
		versions: [{
			additionalPrinterColumns: [{
				jsonPath: ".metadata.creationTimestamp"
				name:     "AGE"
				type:     "date"
			}, {
				jsonPath: ".status.conditions[?(@.type==\"Ready\")].reason"
				name:     "Status"
				type:     "string"
			}]
			name: "v1alpha1"
			schema: openAPIV3Schema: {
				properties: {
					apiVersion: {
						description: "APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources"
						type:        "string"
					}
					kind: {
						description: "Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds"
						type:        "string"
					}
					metadata: type: "object"
					spec: {
						description: "PushSecretSpec configures the behavior of the PushSecret."
						properties: {
							data: {
								description: "Secret Data that should be pushed to providers"
								items: {
									properties: {
										match: {
											description: "Match a given Secret Key to be pushed to the provider."
											properties: {
												remoteRef: {
													description: "Remote Refs to push to providers."
													properties: {
														property: {
															description: "Name of the property in the resulting secret"
															type:        "string"
														}
														remoteKey: {
															description: "Name of the resulting provider secret."
															type:        "string"
														}
													}
													required: ["remoteKey"]
													type: "object"
												}
												secretKey: {
													description: "Secret Key to be pushed"
													type:        "string"
												}
											}
											required: [
												"remoteRef",
												"secretKey",
											]
											type: "object"
										}
										metadata: {
											description:                            "Metadata is metadata attached to the secret. The structure of metadata is provider specific, please look it up in the provider documentation."
											"x-kubernetes-preserve-unknown-fields": true
										}
									}
									required: ["match"]
									type: "object"
								}
								type: "array"
							}
							deletionPolicy: {
								default:     "None"
								description: "Deletion Policy to handle Secrets in the provider. Possible Values: \"Delete/None\". Defaults to \"None\"."
								type:        "string"
							}
							refreshInterval: {
								description: "The Interval to which External Secrets will try to push a secret definition"
								type:        "string"
							}
							secretStoreRefs: {
								items: {
									properties: {
										kind: {
											default:     "SecretStore"
											description: "Kind of the SecretStore resource (SecretStore or ClusterSecretStore) Defaults to `SecretStore`"
											type:        "string"
										}
										labelSelector: {
											description: "Optionally, sync to secret stores with label selector"
											properties: {
												matchExpressions: {
													description: "matchExpressions is a list of label selector requirements. The requirements are ANDed."
													items: {
														description: "A label selector requirement is a selector that contains values, a key, and an operator that relates the key and values."
														properties: {
															key: {
																description: "key is the label key that the selector applies to."
																type:        "string"
															}
															operator: {
																description: "operator represents a key's relationship to a set of values. Valid operators are In, NotIn, Exists and DoesNotExist."
																type:        "string"
															}
															values: {
																description: "values is an array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. This array is replaced during a strategic merge patch."
																items: type: "string"
																type: "array"
															}
														}
														required: [
															"key",
															"operator",
														]
														type: "object"
													}
													type: "array"
												}
												matchLabels: {
													additionalProperties: type: "string"
													description: "matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is \"key\", the operator is \"In\", and the values array contains only \"value\". The requirements are ANDed."
													type:        "object"
												}
											}
											type:                    "object"
											"x-kubernetes-map-type": "atomic"
										}
										name: {
											description: "Optionally, sync to the SecretStore of the given name"
											type:        "string"
										}
									}
									type: "object"
								}
								type: "array"
							}
							selector: {
								description: "The Secret Selector (k8s source) for the Push Secret"
								properties: secret: {
									description: "Select a Secret to Push."
									properties: name: {
										description: "Name of the Secret. The Secret must exist in the same namespace as the PushSecret manifest."
										type:        "string"
									}
									required: ["name"]
									type: "object"
								}
								required: ["secret"]
								type: "object"
							}
						}
						required: [
							"secretStoreRefs",
							"selector",
						]
						type: "object"
					}
					status: {
						description: "PushSecretStatus indicates the history of the status of PushSecret."
						properties: {
							conditions: {
								items: {
									description: "PushSecretStatusCondition indicates the status of the PushSecret."
									properties: {
										lastTransitionTime: {
											format: "date-time"
											type:   "string"
										}
										message: type: "string"
										reason: type:  "string"
										status: type:  "string"
										type: {
											description: "PushSecretConditionType indicates the condition of the PushSecret."
											type:        "string"
										}
									}
									required: [
										"status",
										"type",
									]
									type: "object"
								}
								type: "array"
							}
							refreshTime: {
								description: "refreshTime is the time and date the external secret was fetched and the target secret updated"
								format:      "date-time"
								nullable:    true
								type:        "string"
							}
							syncedPushSecrets: {
								additionalProperties: {
									additionalProperties: {
										properties: {
											match: {
												description: "Match a given Secret Key to be pushed to the provider."
												properties: {
													remoteRef: {
														description: "Remote Refs to push to providers."
														properties: {
															property: {
																description: "Name of the property in the resulting secret"
																type:        "string"
															}
															remoteKey: {
																description: "Name of the resulting provider secret."
																type:        "string"
															}
														}
														required: ["remoteKey"]
														type: "object"
													}
													secretKey: {
														description: "Secret Key to be pushed"
														type:        "string"
													}
												}
												required: [
													"remoteRef",
													"secretKey",
												]
												type: "object"
											}
											metadata: {
												description:                            "Metadata is metadata attached to the secret. The structure of metadata is provider specific, please look it up in the provider documentation."
												"x-kubernetes-preserve-unknown-fields": true
											}
										}
										required: ["match"]
										type: "object"
									}
									type: "object"
								}
								description: "Synced Push Secrets for later deletion. Matches Secret Stores to PushSecretData that was stored to that secretStore."
								type:        "object"
							}
							syncedResourceVersion: {
								description: "SyncedResourceVersion keeps track of the last synced version."
								type:        "string"
							}
						}
						type: "object"
					}
				}
				type: "object"
			}
			served:  true
			storage: true
			subresources: status: {}
		}]
		conversion: {
			strategy: "Webhook"
			webhook: {
				conversionReviewVersions: ["v1"]
				clientConfig: service: {
					name:      "external-secrets-webhook"
					namespace: "external-secrets"
					path:      "/convert"
				}
			}
		}
	}
}, {
	spec: {
		group: "external-secrets.io"
		names: {
			categories: ["externalsecrets"]
			kind:     "SecretStore"
			listKind: "SecretStoreList"
			plural:   "secretstores"
			shortNames: ["ss"]
			singular: "secretstore"
		}
		scope: "Namespaced"
		versions: [{
			additionalPrinterColumns: [{
				jsonPath: ".metadata.creationTimestamp"
				name:     "AGE"
				type:     "date"
			}, {
				jsonPath: ".status.conditions[?(@.type==\"Ready\")].reason"
				name:     "Status"
				type:     "string"
			}]
			deprecated: true
			name:       "v1alpha1"
			schema: openAPIV3Schema: {
				description: "SecretStore represents a secure external location for storing secrets, which can be referenced as part of `storeRef` fields."
				properties: {
					apiVersion: {
						description: "APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources"
						type:        "string"
					}
					kind: {
						description: "Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds"
						type:        "string"
					}
					metadata: type: "object"
					spec: {
						description: "SecretStoreSpec defines the desired state of SecretStore."
						properties: {
							controller: {
								description: "Used to select the correct ESO controller (think: ingress.ingressClassName) The ESO controller is instantiated with a specific controller name and filters ES based on this property"
								type:        "string"
							}
							provider: {
								description:   "Used to configure the provider. Only one provider may be set"
								maxProperties: 1
								minProperties: 1
								properties: {
									akeyless: {
										description: "Akeyless configures this store to sync secrets using Akeyless Vault provider"
										properties: {
											akeylessGWApiURL: {
												description: "Akeyless GW API Url from which the secrets to be fetched from."
												type:        "string"
											}
											authSecretRef: {
												description: "Auth configures how the operator authenticates with Akeyless."
												properties: {
													kubernetesAuth: {
														description: "Kubernetes authenticates with Akeyless by passing the ServiceAccount token stored in the named Secret resource."
														properties: {
															accessID: {
																description: "the Akeyless Kubernetes auth-method access-id"
																type:        "string"
															}
															k8sConfName: {
																description: "Kubernetes-auth configuration name in Akeyless-Gateway"
																type:        "string"
															}
															secretRef: {
																description: "Optional secret field containing a Kubernetes ServiceAccount JWT used for authenticating with Akeyless. If a name is specified without a key, `token` is the default. If one is not specified, the one bound to the controller will be used."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															serviceAccountRef: {
																description: "Optional service account field containing the name of a kubernetes ServiceAccount. If the service account is specified, the service account secret token JWT will be used for authenticating with Akeyless. If the service account selector is not supplied, the secretRef will be used instead."
																properties: {
																	audiences: {
																		description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
																		items: type: "string"
																		type: "array"
																	}
																	name: {
																		description: "The name of the ServiceAccount resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																required: ["name"]
																type: "object"
															}
														}
														required: [
															"accessID",
															"k8sConfName",
														]
														type: "object"
													}
													secretRef: {
														description: "Reference to a Secret that contains the details to authenticate with Akeyless."
														properties: {
															accessID: {
																description: "The SecretAccessID is used for authentication"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															accessType: {
																description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															accessTypeParam: {
																description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														type: "object"
													}
												}
												type: "object"
											}
											caBundle: {
												description: "PEM/base64 encoded CA bundle used to validate Akeyless Gateway certificate. Only used if the AkeylessGWApiURL URL is using HTTPS protocol. If not set the system root certificates are used to validate the TLS connection."
												format:      "byte"
												type:        "string"
											}
											caProvider: {
												description: "The provider for the CA bundle to use to validate Akeyless Gateway certificate."
												properties: {
													key: {
														description: "The key the value inside of the provider type to use, only used with \"Secret\" type"
														type:        "string"
													}
													name: {
														description: "The name of the object located at the provider type."
														type:        "string"
													}
													namespace: {
														description: "The namespace the Provider type is in."
														type:        "string"
													}
													type: {
														description: "The type of provider to use such as \"Secret\", or \"ConfigMap\"."
														enum: [
															"Secret",
															"ConfigMap",
														]
														type: "string"
													}
												}
												required: [
													"name",
													"type",
												]
												type: "object"
											}
										}
										required: [
											"akeylessGWApiURL",
											"authSecretRef",
										]
										type: "object"
									}
									alibaba: {
										description: "Alibaba configures this store to sync secrets using Alibaba Cloud provider"
										properties: {
											auth: {
												description: "AlibabaAuth contains a secretRef for credentials."
												properties: {
													rrsa: {
														description: "Authenticate against Alibaba using RRSA."
														properties: {
															oidcProviderArn: type:   "string"
															oidcTokenFilePath: type: "string"
															roleArn: type:           "string"
															sessionName: type:       "string"
														}
														required: [
															"oidcProviderArn",
															"oidcTokenFilePath",
															"roleArn",
															"sessionName",
														]
														type: "object"
													}
													secretRef: {
														description: "AlibabaAuthSecretRef holds secret references for Alibaba credentials."
														properties: {
															accessKeyIDSecretRef: {
																description: "The AccessKeyID is used for authentication"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															accessKeySecretSecretRef: {
																description: "The AccessKeySecret is used for authentication"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														required: [
															"accessKeyIDSecretRef",
															"accessKeySecretSecretRef",
														]
														type: "object"
													}
												}
												type: "object"
											}
											regionID: {
												description: "Alibaba Region to be used for the provider"
												type:        "string"
											}
										}
										required: [
											"auth",
											"regionID",
										]
										type: "object"
									}
									aws: {
										description: "AWS configures this store to sync secrets using AWS Secret Manager provider"
										properties: {
											auth: {
												description: "Auth defines the information necessary to authenticate against AWS if not set aws sdk will infer credentials from your environment see: https://docs.aws.amazon.com/sdk-for-go/v1/developer-guide/configuring-sdk.html#specifying-credentials"
												properties: {
													jwt: {
														description: "Authenticate against AWS using service account tokens."
														properties: serviceAccountRef: {
															description: "A reference to a ServiceAccount resource."
															properties: {
																audiences: {
																	description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
																	items: type: "string"
																	type: "array"
																}
																name: {
																	description: "The name of the ServiceAccount resource being referred to."
																	type:        "string"
																}
																namespace: {
																	description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																	type:        "string"
																}
															}
															required: ["name"]
															type: "object"
														}
														type: "object"
													}
													secretRef: {
														description: "AWSAuthSecretRef holds secret references for AWS credentials both AccessKeyID and SecretAccessKey must be defined in order to properly authenticate."
														properties: {
															accessKeyIDSecretRef: {
																description: "The AccessKeyID is used for authentication"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															secretAccessKeySecretRef: {
																description: "The SecretAccessKey is used for authentication"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														type: "object"
													}
												}
												type: "object"
											}
											region: {
												description: "AWS Region to be used for the provider"
												type:        "string"
											}
											role: {
												description: "Role is a Role ARN which the SecretManager provider will assume"
												type:        "string"
											}
											service: {
												description: "Service defines which service should be used to fetch the secrets"
												enum: [
													"SecretsManager",
													"ParameterStore",
												]
												type: "string"
											}
										}
										required: [
											"region",
											"service",
										]
										type: "object"
									}
									azurekv: {
										description: "AzureKV configures this store to sync secrets using Azure Key Vault provider"
										properties: {
											authSecretRef: {
												description: "Auth configures how the operator authenticates with Azure. Required for ServicePrincipal auth type."
												properties: {
													clientId: {
														description: "The Azure clientId of the service principle used for authentication."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
													clientSecret: {
														description: "The Azure ClientSecret of the service principle used for authentication."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
												}
												type: "object"
											}
											authType: {
												default:     "ServicePrincipal"
												description: "Auth type defines how to authenticate to the keyvault service. Valid values are: - \"ServicePrincipal\" (default): Using a service principal (tenantId, clientId, clientSecret) - \"ManagedIdentity\": Using Managed Identity assigned to the pod (see aad-pod-identity)"
												enum: [
													"ServicePrincipal",
													"ManagedIdentity",
													"WorkloadIdentity",
												]
												type: "string"
											}
											identityId: {
												description: "If multiple Managed Identity is assigned to the pod, you can select the one to be used"
												type:        "string"
											}
											serviceAccountRef: {
												description: "ServiceAccountRef specified the service account that should be used when authenticating with WorkloadIdentity."
												properties: {
													audiences: {
														description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
														items: type: "string"
														type: "array"
													}
													name: {
														description: "The name of the ServiceAccount resource being referred to."
														type:        "string"
													}
													namespace: {
														description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
														type:        "string"
													}
												}
												required: ["name"]
												type: "object"
											}
											tenantId: {
												description: "TenantID configures the Azure Tenant to send requests to. Required for ServicePrincipal auth type."
												type:        "string"
											}
											vaultUrl: {
												description: "Vault Url from which the secrets to be fetched from."
												type:        "string"
											}
										}
										required: ["vaultUrl"]
										type: "object"
									}
									fake: {
										description: "Fake configures a store with static key/value pairs"
										properties: data: {
											items: {
												properties: {
													key: type:   "string"
													value: type: "string"
													valueMap: {
														additionalProperties: type: "string"
														type: "object"
													}
													version: type: "string"
												}
												required: ["key"]
												type: "object"
											}
											type: "array"
										}
										required: ["data"]
										type: "object"
									}
									gcpsm: {
										description: "GCPSM configures this store to sync secrets using Google Cloud Platform Secret Manager provider"
										properties: {
											auth: {
												description: "Auth defines the information necessary to authenticate against GCP"
												properties: {
													secretRef: {
														properties: secretAccessKeySecretRef: {
															description: "The SecretAccessKey is used for authentication"
															properties: {
																key: {
																	description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																	type:        "string"
																}
																name: {
																	description: "The name of the Secret resource being referred to."
																	type:        "string"
																}
																namespace: {
																	description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																	type:        "string"
																}
															}
															type: "object"
														}
														type: "object"
													}
													workloadIdentity: {
														properties: {
															clusterLocation: type:  "string"
															clusterName: type:      "string"
															clusterProjectID: type: "string"
															serviceAccountRef: {
																description: "A reference to a ServiceAccount resource."
																properties: {
																	audiences: {
																		description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
																		items: type: "string"
																		type: "array"
																	}
																	name: {
																		description: "The name of the ServiceAccount resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																required: ["name"]
																type: "object"
															}
														}
														required: [
															"clusterLocation",
															"clusterName",
															"serviceAccountRef",
														]
														type: "object"
													}
												}
												type: "object"
											}
											projectID: {
												description: "ProjectID project where secret is located"
												type:        "string"
											}
										}
										type: "object"
									}
									gitlab: {
										description: "GitLab configures this store to sync secrets using GitLab Variables provider"
										properties: {
											auth: {
												description: "Auth configures how secret-manager authenticates with a GitLab instance."
												properties: SecretRef: {
													properties: accessToken: {
														description: "AccessToken is used for authentication."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
													type: "object"
												}
												required: ["SecretRef"]
												type: "object"
											}
											projectID: {
												description: "ProjectID specifies a project where secrets are located."
												type:        "string"
											}
											url: {
												description: "URL configures the GitLab instance URL. Defaults to https://gitlab.com/."
												type:        "string"
											}
										}
										required: ["auth"]
										type: "object"
									}
									ibm: {
										description: "IBM configures this store to sync secrets using IBM Cloud provider"
										properties: {
											auth: {
												description: "Auth configures how secret-manager authenticates with the IBM secrets manager."
												properties: secretRef: {
													properties: secretApiKeySecretRef: {
														description: "The SecretAccessKey is used for authentication"
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
													type: "object"
												}
												required: ["secretRef"]
												type: "object"
											}
											serviceUrl: {
												description: "ServiceURL is the Endpoint URL that is specific to the Secrets Manager service instance"
												type:        "string"
											}
										}
										required: ["auth"]
										type: "object"
									}
									kubernetes: {
										description: "Kubernetes configures this store to sync secrets using a Kubernetes cluster provider"
										properties: {
											auth: {
												description:   "Auth configures how secret-manager authenticates with a Kubernetes instance."
												maxProperties: 1
												minProperties: 1
												properties: {
													cert: {
														description: "has both clientCert and clientKey as secretKeySelector"
														properties: {
															clientCert: {
																description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															clientKey: {
																description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														type: "object"
													}
													serviceAccount: {
														description: "points to a service account that should be used for authentication"
														properties: serviceAccount: {
															description: "A reference to a ServiceAccount resource."
															properties: {
																audiences: {
																	description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
																	items: type: "string"
																	type: "array"
																}
																name: {
																	description: "The name of the ServiceAccount resource being referred to."
																	type:        "string"
																}
																namespace: {
																	description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																	type:        "string"
																}
															}
															required: ["name"]
															type: "object"
														}
														type: "object"
													}
													token: {
														description: "use static token to authenticate with"
														properties: bearerToken: {
															description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
															properties: {
																key: {
																	description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																	type:        "string"
																}
																name: {
																	description: "The name of the Secret resource being referred to."
																	type:        "string"
																}
																namespace: {
																	description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																	type:        "string"
																}
															}
															type: "object"
														}
														type: "object"
													}
												}
												type: "object"
											}
											remoteNamespace: {
												default:     "default"
												description: "Remote namespace to fetch the secrets from"
												type:        "string"
											}
											server: {
												description: "configures the Kubernetes server Address."
												properties: {
													caBundle: {
														description: "CABundle is a base64-encoded CA certificate"
														format:      "byte"
														type:        "string"
													}
													caProvider: {
														description: "see: https://external-secrets.io/v0.4.1/spec/#external-secrets.io/v1alpha1.CAProvider"
														properties: {
															key: {
																description: "The key the value inside of the provider type to use, only used with \"Secret\" type"
																type:        "string"
															}
															name: {
																description: "The name of the object located at the provider type."
																type:        "string"
															}
															namespace: {
																description: "The namespace the Provider type is in."
																type:        "string"
															}
															type: {
																description: "The type of provider to use such as \"Secret\", or \"ConfigMap\"."
																enum: [
																	"Secret",
																	"ConfigMap",
																]
																type: "string"
															}
														}
														required: [
															"name",
															"type",
														]
														type: "object"
													}
													url: {
														default:     "kubernetes.default"
														description: "configures the Kubernetes server Address."
														type:        "string"
													}
												}
												type: "object"
											}
										}
										required: ["auth"]
										type: "object"
									}
									oracle: {
										description: "Oracle configures this store to sync secrets using Oracle Vault provider"
										properties: {
											auth: {
												description: "Auth configures how secret-manager authenticates with the Oracle Vault. If empty, use the instance principal, otherwise the user credentials specified in Auth."
												properties: {
													secretRef: {
														description: "SecretRef to pass through sensitive information."
														properties: {
															fingerprint: {
																description: "Fingerprint is the fingerprint of the API private key."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															privatekey: {
																description: "PrivateKey is the user's API Signing Key in PEM format, used for authentication."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														required: [
															"fingerprint",
															"privatekey",
														]
														type: "object"
													}
													tenancy: {
														description: "Tenancy is the tenancy OCID where user is located."
														type:        "string"
													}
													user: {
														description: "User is an access OCID specific to the account."
														type:        "string"
													}
												}
												required: [
													"secretRef",
													"tenancy",
													"user",
												]
												type: "object"
											}
											region: {
												description: "Region is the region where vault is located."
												type:        "string"
											}
											vault: {
												description: "Vault is the vault's OCID of the specific vault where secret is located."
												type:        "string"
											}
										}
										required: [
											"region",
											"vault",
										]
										type: "object"
									}
									vault: {
										description: "Vault configures this store to sync secrets using Hashi provider"
										properties: {
											auth: {
												description: "Auth configures how secret-manager authenticates with the Vault server."
												properties: {
													appRole: {
														description: "AppRole authenticates with Vault using the App Role auth mechanism, with the role and secret stored in a Kubernetes Secret resource."
														properties: {
															path: {
																default:     "approle"
																description: "Path where the App Role authentication backend is mounted in Vault, e.g: \"approle\""
																type:        "string"
															}
															roleId: {
																description: "RoleID configured in the App Role authentication backend when setting up the authentication backend in Vault."
																type:        "string"
															}
															secretRef: {
																description: "Reference to a key in a Secret that contains the App Role secret used to authenticate with Vault. The `key` field must be specified and denotes which entry within the Secret resource is used as the app role secret."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														required: [
															"path",
															"roleId",
															"secretRef",
														]
														type: "object"
													}
													cert: {
														description: "Cert authenticates with TLS Certificates by passing client certificate, private key and ca certificate Cert authentication method"
														properties: {
															clientCert: {
																description: "ClientCert is a certificate to authenticate using the Cert Vault authentication method"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															secretRef: {
																description: "SecretRef to a key in a Secret resource containing client private key to authenticate with Vault using the Cert authentication method"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														type: "object"
													}
													jwt: {
														description: "Jwt authenticates with Vault by passing role and JWT token using the JWT/OIDC authentication method"
														properties: {
															kubernetesServiceAccountToken: {
																description: "Optional ServiceAccountToken specifies the Kubernetes service account for which to request a token for with the `TokenRequest` API."
																properties: {
																	audiences: {
																		description: "Optional audiences field that will be used to request a temporary Kubernetes service account token for the service account referenced by `serviceAccountRef`. Defaults to a single audience `vault` it not specified."
																		items: type: "string"
																		type: "array"
																	}
																	expirationSeconds: {
																		description: "Optional expiration time in seconds that will be used to request a temporary Kubernetes service account token for the service account referenced by `serviceAccountRef`. Defaults to 10 minutes."
																		format:      "int64"
																		type:        "integer"
																	}
																	serviceAccountRef: {
																		description: "Service account field containing the name of a kubernetes ServiceAccount."
																		properties: {
																			audiences: {
																				description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
																				items: type: "string"
																				type: "array"
																			}
																			name: {
																				description: "The name of the ServiceAccount resource being referred to."
																				type:        "string"
																			}
																			namespace: {
																				description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																				type:        "string"
																			}
																		}
																		required: ["name"]
																		type: "object"
																	}
																}
																required: ["serviceAccountRef"]
																type: "object"
															}
															path: {
																default:     "jwt"
																description: "Path where the JWT authentication backend is mounted in Vault, e.g: \"jwt\""
																type:        "string"
															}
															role: {
																description: "Role is a JWT role to authenticate using the JWT/OIDC Vault authentication method"
																type:        "string"
															}
															secretRef: {
																description: "Optional SecretRef that refers to a key in a Secret resource containing JWT token to authenticate with Vault using the JWT/OIDC authentication method."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														required: ["path"]
														type: "object"
													}
													kubernetes: {
														description: "Kubernetes authenticates with Vault by passing the ServiceAccount token stored in the named Secret resource to the Vault server."
														properties: {
															mountPath: {
																default:     "kubernetes"
																description: "Path where the Kubernetes authentication backend is mounted in Vault, e.g: \"kubernetes\""
																type:        "string"
															}
															role: {
																description: "A required field containing the Vault Role to assume. A Role binds a Kubernetes ServiceAccount with a set of Vault policies."
																type:        "string"
															}
															secretRef: {
																description: "Optional secret field containing a Kubernetes ServiceAccount JWT used for authenticating with Vault. If a name is specified without a key, `token` is the default. If one is not specified, the one bound to the controller will be used."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															serviceAccountRef: {
																description: "Optional service account field containing the name of a kubernetes ServiceAccount. If the service account is specified, the service account secret token JWT will be used for authenticating with Vault. If the service account selector is not supplied, the secretRef will be used instead."
																properties: {
																	audiences: {
																		description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
																		items: type: "string"
																		type: "array"
																	}
																	name: {
																		description: "The name of the ServiceAccount resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																required: ["name"]
																type: "object"
															}
														}
														required: [
															"mountPath",
															"role",
														]
														type: "object"
													}
													ldap: {
														description: "Ldap authenticates with Vault by passing username/password pair using the LDAP authentication method"
														properties: {
															path: {
																default:     "ldap"
																description: "Path where the LDAP authentication backend is mounted in Vault, e.g: \"ldap\""
																type:        "string"
															}
															secretRef: {
																description: "SecretRef to a key in a Secret resource containing password for the LDAP user used to authenticate with Vault using the LDAP authentication method"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															username: {
																description: "Username is a LDAP user name used to authenticate using the LDAP Vault authentication method"
																type:        "string"
															}
														}
														required: [
															"path",
															"username",
														]
														type: "object"
													}
													tokenSecretRef: {
														description: "TokenSecretRef authenticates with Vault by presenting a token."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
												}
												type: "object"
											}
											caBundle: {
												description: "PEM encoded CA bundle used to validate Vault server certificate. Only used if the Server URL is using HTTPS protocol. This parameter is ignored for plain HTTP protocol connection. If not set the system root certificates are used to validate the TLS connection."
												format:      "byte"
												type:        "string"
											}
											caProvider: {
												description: "The provider for the CA bundle to use to validate Vault server certificate."
												properties: {
													key: {
														description: "The key the value inside of the provider type to use, only used with \"Secret\" type"
														type:        "string"
													}
													name: {
														description: "The name of the object located at the provider type."
														type:        "string"
													}
													namespace: {
														description: "The namespace the Provider type is in."
														type:        "string"
													}
													type: {
														description: "The type of provider to use such as \"Secret\", or \"ConfigMap\"."
														enum: [
															"Secret",
															"ConfigMap",
														]
														type: "string"
													}
												}
												required: [
													"name",
													"type",
												]
												type: "object"
											}
											forwardInconsistent: {
												description: "ForwardInconsistent tells Vault to forward read-after-write requests to the Vault leader instead of simply retrying within a loop. This can increase performance if the option is enabled serverside. https://www.vaultproject.io/docs/configuration/replication#allow_forwarding_via_header"
												type:        "boolean"
											}
											namespace: {
												description: "Name of the vault namespace. Namespaces is a set of features within Vault Enterprise that allows Vault environments to support Secure Multi-tenancy. e.g: \"ns1\". More about namespaces can be found here https://www.vaultproject.io/docs/enterprise/namespaces"
												type:        "string"
											}
											path: {
												description: "Path is the mount path of the Vault KV backend endpoint, e.g: \"secret\". The v2 KV secret engine version specific \"/data\" path suffix for fetching secrets from Vault is optional and will be appended if not present in specified path."
												type:        "string"
											}
											readYourWrites: {
												description: "ReadYourWrites ensures isolated read-after-write semantics by providing discovered cluster replication states in each request. More information about eventual consistency in Vault can be found here https://www.vaultproject.io/docs/enterprise/consistency"
												type:        "boolean"
											}
											server: {
												description: "Server is the connection address for the Vault server, e.g: \"https://vault.example.com:8200\"."
												type:        "string"
											}
											version: {
												default:     "v2"
												description: "Version is the Vault KV secret engine version. This can be either \"v1\" or \"v2\". Version defaults to \"v2\"."
												enum: [
													"v1",
													"v2",
												]
												type: "string"
											}
										}
										required: [
											"auth",
											"server",
										]
										type: "object"
									}
									webhook: {
										description: "Webhook configures this store to sync secrets using a generic templated webhook"
										properties: {
											body: {
												description: "Body"
												type:        "string"
											}
											caBundle: {
												description: "PEM encoded CA bundle used to validate webhook server certificate. Only used if the Server URL is using HTTPS protocol. This parameter is ignored for plain HTTP protocol connection. If not set the system root certificates are used to validate the TLS connection."
												format:      "byte"
												type:        "string"
											}
											caProvider: {
												description: "The provider for the CA bundle to use to validate webhook server certificate."
												properties: {
													key: {
														description: "The key the value inside of the provider type to use, only used with \"Secret\" type"
														type:        "string"
													}
													name: {
														description: "The name of the object located at the provider type."
														type:        "string"
													}
													namespace: {
														description: "The namespace the Provider type is in."
														type:        "string"
													}
													type: {
														description: "The type of provider to use such as \"Secret\", or \"ConfigMap\"."
														enum: [
															"Secret",
															"ConfigMap",
														]
														type: "string"
													}
												}
												required: [
													"name",
													"type",
												]
												type: "object"
											}
											headers: {
												additionalProperties: type: "string"
												description: "Headers"
												type:        "object"
											}
											method: {
												description: "Webhook Method"
												type:        "string"
											}
											result: {
												description: "Result formatting"
												properties: jsonPath: {
													description: "Json path of return value"
													type:        "string"
												}
												type: "object"
											}
											secrets: {
												description: "Secrets to fill in templates These secrets will be passed to the templating function as key value pairs under the given name"
												items: {
													properties: {
														name: {
															description: "Name of this secret in templates"
															type:        "string"
														}
														secretRef: {
															description: "Secret ref to fill in credentials"
															properties: {
																key: {
																	description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																	type:        "string"
																}
																name: {
																	description: "The name of the Secret resource being referred to."
																	type:        "string"
																}
																namespace: {
																	description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																	type:        "string"
																}
															}
															type: "object"
														}
													}
													required: [
														"name",
														"secretRef",
													]
													type: "object"
												}
												type: "array"
											}
											timeout: {
												description: "Timeout"
												type:        "string"
											}
											url: {
												description: "Webhook url to call"
												type:        "string"
											}
										}
										required: [
											"result",
											"url",
										]
										type: "object"
									}
									yandexlockbox: {
										description: "YandexLockbox configures this store to sync secrets using Yandex Lockbox provider"
										properties: {
											apiEndpoint: {
												description: "Yandex.Cloud API endpoint (e.g. 'api.cloud.yandex.net:443')"
												type:        "string"
											}
											auth: {
												description: "Auth defines the information necessary to authenticate against Yandex Lockbox"
												properties: authorizedKeySecretRef: {
													description: "The authorized key used for authentication"
													properties: {
														key: {
															description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
															type:        "string"
														}
														name: {
															description: "The name of the Secret resource being referred to."
															type:        "string"
														}
														namespace: {
															description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
															type:        "string"
														}
													}
													type: "object"
												}
												type: "object"
											}
											caProvider: {
												description: "The provider for the CA bundle to use to validate Yandex.Cloud server certificate."
												properties: certSecretRef: {
													description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
													properties: {
														key: {
															description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
															type:        "string"
														}
														name: {
															description: "The name of the Secret resource being referred to."
															type:        "string"
														}
														namespace: {
															description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
															type:        "string"
														}
													}
													type: "object"
												}
												type: "object"
											}
										}
										required: ["auth"]
										type: "object"
									}
								}
								type: "object"
							}
							retrySettings: {
								description: "Used to configure http retries if failed"
								properties: {
									maxRetries: {
										format: "int32"
										type:   "integer"
									}
									retryInterval: type: "string"
								}
								type: "object"
							}
						}
						required: ["provider"]
						type: "object"
					}
					status: {
						description: "SecretStoreStatus defines the observed state of the SecretStore."
						properties: conditions: {
							items: {
								properties: {
									lastTransitionTime: {
										format: "date-time"
										type:   "string"
									}
									message: type: "string"
									reason: type:  "string"
									status: type:  "string"
									type: type:    "string"
								}
								required: [
									"status",
									"type",
								]
								type: "object"
							}
							type: "array"
						}
						type: "object"
					}
				}
				type: "object"
			}
			served:  true
			storage: false
			subresources: status: {}
		}, {
			additionalPrinterColumns: [{
				jsonPath: ".metadata.creationTimestamp"
				name:     "AGE"
				type:     "date"
			}, {
				jsonPath: ".status.conditions[?(@.type==\"Ready\")].reason"
				name:     "Status"
				type:     "string"
			}, {
				jsonPath: ".status.capabilities"
				name:     "Capabilities"
				type:     "string"
			}, {
				jsonPath: ".status.conditions[?(@.type==\"Ready\")].status"
				name:     "Ready"
				type:     "string"
			}]
			name: "v1beta1"
			schema: openAPIV3Schema: {
				description: "SecretStore represents a secure external location for storing secrets, which can be referenced as part of `storeRef` fields."
				properties: {
					apiVersion: {
						description: "APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources"
						type:        "string"
					}
					kind: {
						description: "Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds"
						type:        "string"
					}
					metadata: type: "object"
					spec: {
						description: "SecretStoreSpec defines the desired state of SecretStore."
						properties: {
							conditions: {
								description: "Used to constraint a ClusterSecretStore to specific namespaces. Relevant only to ClusterSecretStore"
								items: {
									description: "ClusterSecretStoreCondition describes a condition by which to choose namespaces to process ExternalSecrets in for a ClusterSecretStore instance."
									properties: {
										namespaceSelector: {
											description: "Choose namespace using a labelSelector"
											properties: {
												matchExpressions: {
													description: "matchExpressions is a list of label selector requirements. The requirements are ANDed."
													items: {
														description: "A label selector requirement is a selector that contains values, a key, and an operator that relates the key and values."
														properties: {
															key: {
																description: "key is the label key that the selector applies to."
																type:        "string"
															}
															operator: {
																description: "operator represents a key's relationship to a set of values. Valid operators are In, NotIn, Exists and DoesNotExist."
																type:        "string"
															}
															values: {
																description: "values is an array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. This array is replaced during a strategic merge patch."
																items: type: "string"
																type: "array"
															}
														}
														required: [
															"key",
															"operator",
														]
														type: "object"
													}
													type: "array"
												}
												matchLabels: {
													additionalProperties: type: "string"
													description: "matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is \"key\", the operator is \"In\", and the values array contains only \"value\". The requirements are ANDed."
													type:        "object"
												}
											}
											type:                    "object"
											"x-kubernetes-map-type": "atomic"
										}
										namespaces: {
											description: "Choose namespaces by name"
											items: type: "string"
											type: "array"
										}
									}
									type: "object"
								}
								type: "array"
							}
							controller: {
								description: "Used to select the correct ESO controller (think: ingress.ingressClassName) The ESO controller is instantiated with a specific controller name and filters ES based on this property"
								type:        "string"
							}
							provider: {
								description:   "Used to configure the provider. Only one provider may be set"
								maxProperties: 1
								minProperties: 1
								properties: {
									akeyless: {
										description: "Akeyless configures this store to sync secrets using Akeyless Vault provider"
										properties: {
											akeylessGWApiURL: {
												description: "Akeyless GW API Url from which the secrets to be fetched from."
												type:        "string"
											}
											authSecretRef: {
												description: "Auth configures how the operator authenticates with Akeyless."
												properties: {
													kubernetesAuth: {
														description: "Kubernetes authenticates with Akeyless by passing the ServiceAccount token stored in the named Secret resource."
														properties: {
															accessID: {
																description: "the Akeyless Kubernetes auth-method access-id"
																type:        "string"
															}
															k8sConfName: {
																description: "Kubernetes-auth configuration name in Akeyless-Gateway"
																type:        "string"
															}
															secretRef: {
																description: "Optional secret field containing a Kubernetes ServiceAccount JWT used for authenticating with Akeyless. If a name is specified without a key, `token` is the default. If one is not specified, the one bound to the controller will be used."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															serviceAccountRef: {
																description: "Optional service account field containing the name of a kubernetes ServiceAccount. If the service account is specified, the service account secret token JWT will be used for authenticating with Akeyless. If the service account selector is not supplied, the secretRef will be used instead."
																properties: {
																	audiences: {
																		description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
																		items: type: "string"
																		type: "array"
																	}
																	name: {
																		description: "The name of the ServiceAccount resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																required: ["name"]
																type: "object"
															}
														}
														required: [
															"accessID",
															"k8sConfName",
														]
														type: "object"
													}
													secretRef: {
														description: "Reference to a Secret that contains the details to authenticate with Akeyless."
														properties: {
															accessID: {
																description: "The SecretAccessID is used for authentication"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															accessType: {
																description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															accessTypeParam: {
																description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														type: "object"
													}
												}
												type: "object"
											}
											caBundle: {
												description: "PEM/base64 encoded CA bundle used to validate Akeyless Gateway certificate. Only used if the AkeylessGWApiURL URL is using HTTPS protocol. If not set the system root certificates are used to validate the TLS connection."
												format:      "byte"
												type:        "string"
											}
											caProvider: {
												description: "The provider for the CA bundle to use to validate Akeyless Gateway certificate."
												properties: {
													key: {
														description: "The key where the CA certificate can be found in the Secret or ConfigMap."
														type:        "string"
													}
													name: {
														description: "The name of the object located at the provider type."
														type:        "string"
													}
													namespace: {
														description: "The namespace the Provider type is in. Can only be defined when used in a ClusterSecretStore."
														type:        "string"
													}
													type: {
														description: "The type of provider to use such as \"Secret\", or \"ConfigMap\"."
														enum: [
															"Secret",
															"ConfigMap",
														]
														type: "string"
													}
												}
												required: [
													"name",
													"type",
												]
												type: "object"
											}
										}
										required: [
											"akeylessGWApiURL",
											"authSecretRef",
										]
										type: "object"
									}
									alibaba: {
										description: "Alibaba configures this store to sync secrets using Alibaba Cloud provider"
										properties: {
											auth: {
												description: "AlibabaAuth contains a secretRef for credentials."
												properties: {
													rrsa: {
														description: "Authenticate against Alibaba using RRSA."
														properties: {
															oidcProviderArn: type:   "string"
															oidcTokenFilePath: type: "string"
															roleArn: type:           "string"
															sessionName: type:       "string"
														}
														required: [
															"oidcProviderArn",
															"oidcTokenFilePath",
															"roleArn",
															"sessionName",
														]
														type: "object"
													}
													secretRef: {
														description: "AlibabaAuthSecretRef holds secret references for Alibaba credentials."
														properties: {
															accessKeyIDSecretRef: {
																description: "The AccessKeyID is used for authentication"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															accessKeySecretSecretRef: {
																description: "The AccessKeySecret is used for authentication"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														required: [
															"accessKeyIDSecretRef",
															"accessKeySecretSecretRef",
														]
														type: "object"
													}
												}
												type: "object"
											}
											regionID: {
												description: "Alibaba Region to be used for the provider"
												type:        "string"
											}
										}
										required: [
											"auth",
											"regionID",
										]
										type: "object"
									}
									aws: {
										description: "AWS configures this store to sync secrets using AWS Secret Manager provider"
										properties: {
											additionalRoles: {
												description: "AdditionalRoles is a chained list of Role ARNs which the SecretManager provider will sequentially assume before assuming Role"
												items: type: "string"
												type: "array"
											}
											auth: {
												description: "Auth defines the information necessary to authenticate against AWS if not set aws sdk will infer credentials from your environment see: https://docs.aws.amazon.com/sdk-for-go/v1/developer-guide/configuring-sdk.html#specifying-credentials"
												properties: {
													jwt: {
														description: "Authenticate against AWS using service account tokens."
														properties: serviceAccountRef: {
															description: "A reference to a ServiceAccount resource."
															properties: {
																audiences: {
																	description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
																	items: type: "string"
																	type: "array"
																}
																name: {
																	description: "The name of the ServiceAccount resource being referred to."
																	type:        "string"
																}
																namespace: {
																	description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																	type:        "string"
																}
															}
															required: ["name"]
															type: "object"
														}
														type: "object"
													}
													secretRef: {
														description: "AWSAuthSecretRef holds secret references for AWS credentials both AccessKeyID and SecretAccessKey must be defined in order to properly authenticate."
														properties: {
															accessKeyIDSecretRef: {
																description: "The AccessKeyID is used for authentication"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															secretAccessKeySecretRef: {
																description: "The SecretAccessKey is used for authentication"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															sessionTokenSecretRef: {
																description: "The SessionToken used for authentication This must be defined if AccessKeyID and SecretAccessKey are temporary credentials see: https://docs.aws.amazon.com/IAM/latest/UserGuide/id_credentials_temp_use-resources.html"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														type: "object"
													}
												}
												type: "object"
											}
											externalID: {
												description: "AWS External ID set on assumed IAM roles"
												type:        "string"
											}
											region: {
												description: "AWS Region to be used for the provider"
												type:        "string"
											}
											role: {
												description: "Role is a Role ARN which the SecretManager provider will assume"
												type:        "string"
											}
											service: {
												description: "Service defines which service should be used to fetch the secrets"
												enum: [
													"SecretsManager",
													"ParameterStore",
												]
												type: "string"
											}
											sessionTags: {
												description: "AWS STS assume role session tags"
												items: {
													properties: {
														key: type:   "string"
														value: type: "string"
													}
													required: [
														"key",
														"value",
													]
													type: "object"
												}
												type: "array"
											}
											transitiveTagKeys: {
												description: "AWS STS assume role transitive session tags. Required when multiple rules are used with SecretStore"
												items: type: "string"
												type: "array"
											}
										}
										required: [
											"region",
											"service",
										]
										type: "object"
									}
									azurekv: {
										description: "AzureKV configures this store to sync secrets using Azure Key Vault provider"
										properties: {
											authSecretRef: {
												description: "Auth configures how the operator authenticates with Azure. Required for ServicePrincipal auth type."
												properties: {
													clientId: {
														description: "The Azure clientId of the service principle used for authentication."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
													clientSecret: {
														description: "The Azure ClientSecret of the service principle used for authentication."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
												}
												type: "object"
											}
											authType: {
												default:     "ServicePrincipal"
												description: "Auth type defines how to authenticate to the keyvault service. Valid values are: - \"ServicePrincipal\" (default): Using a service principal (tenantId, clientId, clientSecret) - \"ManagedIdentity\": Using Managed Identity assigned to the pod (see aad-pod-identity)"
												enum: [
													"ServicePrincipal",
													"ManagedIdentity",
													"WorkloadIdentity",
												]
												type: "string"
											}
											environmentType: {
												default:     "PublicCloud"
												description: "EnvironmentType specifies the Azure cloud environment endpoints to use for connecting and authenticating with Azure. By default it points to the public cloud AAD endpoint. The following endpoints are available, also see here: https://github.com/Azure/go-autorest/blob/main/autorest/azure/environments.go#L152 PublicCloud, USGovernmentCloud, ChinaCloud, GermanCloud"
												enum: [
													"PublicCloud",
													"USGovernmentCloud",
													"ChinaCloud",
													"GermanCloud",
												]
												type: "string"
											}
											identityId: {
												description: "If multiple Managed Identity is assigned to the pod, you can select the one to be used"
												type:        "string"
											}
											serviceAccountRef: {
												description: "ServiceAccountRef specified the service account that should be used when authenticating with WorkloadIdentity."
												properties: {
													audiences: {
														description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
														items: type: "string"
														type: "array"
													}
													name: {
														description: "The name of the ServiceAccount resource being referred to."
														type:        "string"
													}
													namespace: {
														description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
														type:        "string"
													}
												}
												required: ["name"]
												type: "object"
											}
											tenantId: {
												description: "TenantID configures the Azure Tenant to send requests to. Required for ServicePrincipal auth type."
												type:        "string"
											}
											vaultUrl: {
												description: "Vault Url from which the secrets to be fetched from."
												type:        "string"
											}
										}
										required: ["vaultUrl"]
										type: "object"
									}
									conjur: {
										description: "Conjur configures this store to sync secrets using conjur provider"
										properties: {
											auth: {
												properties: apikey: {
													properties: {
														account: type: "string"
														apiKeyRef: {
															description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
															properties: {
																key: {
																	description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																	type:        "string"
																}
																name: {
																	description: "The name of the Secret resource being referred to."
																	type:        "string"
																}
																namespace: {
																	description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																	type:        "string"
																}
															}
															type: "object"
														}
														userRef: {
															description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
															properties: {
																key: {
																	description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																	type:        "string"
																}
																name: {
																	description: "The name of the Secret resource being referred to."
																	type:        "string"
																}
																namespace: {
																	description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																	type:        "string"
																}
															}
															type: "object"
														}
													}
													required: [
														"account",
														"apiKeyRef",
														"userRef",
													]
													type: "object"
												}
												required: ["apikey"]
												type: "object"
											}
											caBundle: type: "string"
											url: type:      "string"
										}
										required: [
											"auth",
											"url",
										]
										type: "object"
									}
									delinea: {
										description: "Delinea DevOps Secrets Vault https://docs.delinea.com/online-help/products/devops-secrets-vault/current"
										properties: {
											clientId: {
												description: "ClientID is the non-secret part of the credential."
												properties: {
													secretRef: {
														description: "SecretRef references a key in a secret that will be used as value."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
													value: {
														description: "Value can be specified directly to set a value without using a secret."
														type:        "string"
													}
												}
												type: "object"
											}
											clientSecret: {
												description: "ClientSecret is the secret part of the credential."
												properties: {
													secretRef: {
														description: "SecretRef references a key in a secret that will be used as value."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
													value: {
														description: "Value can be specified directly to set a value without using a secret."
														type:        "string"
													}
												}
												type: "object"
											}
											tenant: {
												description: "Tenant is the chosen hostname / site name."
												type:        "string"
											}
											tld: {
												description: "TLD is based on the server location that was chosen during provisioning. If unset, defaults to \"com\"."
												type:        "string"
											}
											urlTemplate: {
												description: "URLTemplate If unset, defaults to \"https://%s.secretsvaultcloud.%s/v1/%s%s\"."
												type:        "string"
											}
										}
										required: [
											"clientId",
											"clientSecret",
											"tenant",
										]
										type: "object"
									}
									doppler: {
										description: "Doppler configures this store to sync secrets using the Doppler provider"
										properties: {
											auth: {
												description: "Auth configures how the Operator authenticates with the Doppler API"
												properties: secretRef: {
													properties: dopplerToken: {
														description: "The DopplerToken is used for authentication. See https://docs.doppler.com/reference/api#authentication for auth token types. The Key attribute defaults to dopplerToken if not specified."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
													required: ["dopplerToken"]
													type: "object"
												}
												required: ["secretRef"]
												type: "object"
											}
											config: {
												description: "Doppler config (required if not using a Service Token)"
												type:        "string"
											}
											format: {
												description: "Format enables the downloading of secrets as a file (string)"
												enum: [
													"json",
													"dotnet-json",
													"env",
													"yaml",
													"docker",
												]
												type: "string"
											}
											nameTransformer: {
												description: "Environment variable compatible name transforms that change secret names to a different format"
												enum: [
													"upper-camel",
													"camel",
													"lower-snake",
													"tf-var",
													"dotnet-env",
													"lower-kebab",
												]
												type: "string"
											}
											project: {
												description: "Doppler project (required if not using a Service Token)"
												type:        "string"
											}
										}
										required: ["auth"]
										type: "object"
									}
									fake: {
										description: "Fake configures a store with static key/value pairs"
										properties: data: {
											items: {
												properties: {
													key: type:   "string"
													value: type: "string"
													valueMap: {
														additionalProperties: type: "string"
														type: "object"
													}
													version: type: "string"
												}
												required: ["key"]
												type: "object"
											}
											type: "array"
										}
										required: ["data"]
										type: "object"
									}
									gcpsm: {
										description: "GCPSM configures this store to sync secrets using Google Cloud Platform Secret Manager provider"
										properties: {
											auth: {
												description: "Auth defines the information necessary to authenticate against GCP"
												properties: {
													secretRef: {
														properties: secretAccessKeySecretRef: {
															description: "The SecretAccessKey is used for authentication"
															properties: {
																key: {
																	description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																	type:        "string"
																}
																name: {
																	description: "The name of the Secret resource being referred to."
																	type:        "string"
																}
																namespace: {
																	description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																	type:        "string"
																}
															}
															type: "object"
														}
														type: "object"
													}
													workloadIdentity: {
														properties: {
															clusterLocation: type:  "string"
															clusterName: type:      "string"
															clusterProjectID: type: "string"
															serviceAccountRef: {
																description: "A reference to a ServiceAccount resource."
																properties: {
																	audiences: {
																		description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
																		items: type: "string"
																		type: "array"
																	}
																	name: {
																		description: "The name of the ServiceAccount resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																required: ["name"]
																type: "object"
															}
														}
														required: [
															"clusterLocation",
															"clusterName",
															"serviceAccountRef",
														]
														type: "object"
													}
												}
												type: "object"
											}
											projectID: {
												description: "ProjectID project where secret is located"
												type:        "string"
											}
										}
										type: "object"
									}
									gitlab: {
										description: "GitLab configures this store to sync secrets using GitLab Variables provider"
										properties: {
											auth: {
												description: "Auth configures how secret-manager authenticates with a GitLab instance."
												properties: SecretRef: {
													properties: accessToken: {
														description: "AccessToken is used for authentication."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
													type: "object"
												}
												required: ["SecretRef"]
												type: "object"
											}
											environment: {
												description: "Environment environment_scope of gitlab CI/CD variables (Please see https://docs.gitlab.com/ee/ci/environments/#create-a-static-environment on how to create environments)"
												type:        "string"
											}
											groupIDs: {
												description: "GroupIDs specify, which gitlab groups to pull secrets from. Group secrets are read from left to right followed by the project variables."
												items: type: "string"
												type: "array"
											}
											inheritFromGroups: {
												description: "InheritFromGroups specifies whether parent groups should be discovered and checked for secrets."
												type:        "boolean"
											}
											projectID: {
												description: "ProjectID specifies a project where secrets are located."
												type:        "string"
											}
											url: {
												description: "URL configures the GitLab instance URL. Defaults to https://gitlab.com/."
												type:        "string"
											}
										}
										required: ["auth"]
										type: "object"
									}
									ibm: {
										description: "IBM configures this store to sync secrets using IBM Cloud provider"
										properties: {
											auth: {
												description:   "Auth configures how secret-manager authenticates with the IBM secrets manager."
												maxProperties: 1
												minProperties: 1
												properties: {
													containerAuth: {
														description: "IBM Container-based auth with IAM Trusted Profile."
														properties: {
															iamEndpoint: type: "string"
															profile: {
																description: "the IBM Trusted Profile"
																type:        "string"
															}
															tokenLocation: {
																description: "Location the token is mounted on the pod"
																type:        "string"
															}
														}
														required: ["profile"]
														type: "object"
													}
													secretRef: {
														properties: secretApiKeySecretRef: {
															description: "The SecretAccessKey is used for authentication"
															properties: {
																key: {
																	description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																	type:        "string"
																}
																name: {
																	description: "The name of the Secret resource being referred to."
																	type:        "string"
																}
																namespace: {
																	description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																	type:        "string"
																}
															}
															type: "object"
														}
														type: "object"
													}
												}
												type: "object"
											}
											serviceUrl: {
												description: "ServiceURL is the Endpoint URL that is specific to the Secrets Manager service instance"
												type:        "string"
											}
										}
										required: ["auth"]
										type: "object"
									}
									keepersecurity: {
										description: "KeeperSecurity configures this store to sync secrets using the KeeperSecurity provider"
										properties: {
											authRef: {
												description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
												properties: {
													key: {
														description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
														type:        "string"
													}
													name: {
														description: "The name of the Secret resource being referred to."
														type:        "string"
													}
													namespace: {
														description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
														type:        "string"
													}
												}
												type: "object"
											}
											folderID: type: "string"
										}
										required: [
											"authRef",
											"folderID",
										]
										type: "object"
									}
									kubernetes: {
										description: "Kubernetes configures this store to sync secrets using a Kubernetes cluster provider"
										properties: {
											auth: {
												description:   "Auth configures how secret-manager authenticates with a Kubernetes instance."
												maxProperties: 1
												minProperties: 1
												properties: {
													cert: {
														description: "has both clientCert and clientKey as secretKeySelector"
														properties: {
															clientCert: {
																description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															clientKey: {
																description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														type: "object"
													}
													serviceAccount: {
														description: "points to a service account that should be used for authentication"
														properties: {
															audiences: {
																description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
																items: type: "string"
																type: "array"
															}
															name: {
																description: "The name of the ServiceAccount resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														required: ["name"]
														type: "object"
													}
													token: {
														description: "use static token to authenticate with"
														properties: bearerToken: {
															description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
															properties: {
																key: {
																	description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																	type:        "string"
																}
																name: {
																	description: "The name of the Secret resource being referred to."
																	type:        "string"
																}
																namespace: {
																	description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																	type:        "string"
																}
															}
															type: "object"
														}
														type: "object"
													}
												}
												type: "object"
											}
											remoteNamespace: {
												default:     "default"
												description: "Remote namespace to fetch the secrets from"
												type:        "string"
											}
											server: {
												description: "configures the Kubernetes server Address."
												properties: {
													caBundle: {
														description: "CABundle is a base64-encoded CA certificate"
														format:      "byte"
														type:        "string"
													}
													caProvider: {
														description: "see: https://external-secrets.io/v0.4.1/spec/#external-secrets.io/v1alpha1.CAProvider"
														properties: {
															key: {
																description: "The key where the CA certificate can be found in the Secret or ConfigMap."
																type:        "string"
															}
															name: {
																description: "The name of the object located at the provider type."
																type:        "string"
															}
															namespace: {
																description: "The namespace the Provider type is in. Can only be defined when used in a ClusterSecretStore."
																type:        "string"
															}
															type: {
																description: "The type of provider to use such as \"Secret\", or \"ConfigMap\"."
																enum: [
																	"Secret",
																	"ConfigMap",
																]
																type: "string"
															}
														}
														required: [
															"name",
															"type",
														]
														type: "object"
													}
													url: {
														default:     "kubernetes.default"
														description: "configures the Kubernetes server Address."
														type:        "string"
													}
												}
												type: "object"
											}
										}
										required: ["auth"]
										type: "object"
									}
									onepassword: {
										description: "OnePassword configures this store to sync secrets using the 1Password Cloud provider"
										properties: {
											auth: {
												description: "Auth defines the information necessary to authenticate against OnePassword Connect Server"
												properties: secretRef: {
													description: "OnePasswordAuthSecretRef holds secret references for 1Password credentials."
													properties: connectTokenSecretRef: {
														description: "The ConnectToken is used for authentication to a 1Password Connect Server."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
													required: ["connectTokenSecretRef"]
													type: "object"
												}
												required: ["secretRef"]
												type: "object"
											}
											connectHost: {
												description: "ConnectHost defines the OnePassword Connect Server to connect to"
												type:        "string"
											}
											vaults: {
												additionalProperties: type: "integer"
												description: "Vaults defines which OnePassword vaults to search in which order"
												type:        "object"
											}
										}
										required: [
											"auth",
											"connectHost",
											"vaults",
										]
										type: "object"
									}
									oracle: {
										description: "Oracle configures this store to sync secrets using Oracle Vault provider"
										properties: {
											auth: {
												description: "Auth configures how secret-manager authenticates with the Oracle Vault. If empty, use the instance principal, otherwise the user credentials specified in Auth."
												properties: {
													secretRef: {
														description: "SecretRef to pass through sensitive information."
														properties: {
															fingerprint: {
																description: "Fingerprint is the fingerprint of the API private key."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															privatekey: {
																description: "PrivateKey is the user's API Signing Key in PEM format, used for authentication."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														required: [
															"fingerprint",
															"privatekey",
														]
														type: "object"
													}
													tenancy: {
														description: "Tenancy is the tenancy OCID where user is located."
														type:        "string"
													}
													user: {
														description: "User is an access OCID specific to the account."
														type:        "string"
													}
												}
												required: [
													"secretRef",
													"tenancy",
													"user",
												]
												type: "object"
											}
											region: {
												description: "Region is the region where vault is located."
												type:        "string"
											}
											vault: {
												description: "Vault is the vault's OCID of the specific vault where secret is located."
												type:        "string"
											}
										}
										required: [
											"region",
											"vault",
										]
										type: "object"
									}
									scaleway: {
										description: "Scaleway"
										properties: {
											accessKey: {
												description: "AccessKey is the non-secret part of the api key."
												properties: {
													secretRef: {
														description: "SecretRef references a key in a secret that will be used as value."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
													value: {
														description: "Value can be specified directly to set a value without using a secret."
														type:        "string"
													}
												}
												type: "object"
											}
											apiUrl: {
												description: "APIURL is the url of the api to use. Defaults to https://api.scaleway.com"
												type:        "string"
											}
											projectId: {
												description: "ProjectID is the id of your project, which you can find in the console: https://console.scaleway.com/project/settings"
												type:        "string"
											}
											region: {
												description: "Region where your secrets are located: https://developers.scaleway.com/en/quickstart/#region-and-zone"
												type:        "string"
											}
											secretKey: {
												description: "SecretKey is the non-secret part of the api key."
												properties: {
													secretRef: {
														description: "SecretRef references a key in a secret that will be used as value."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
													value: {
														description: "Value can be specified directly to set a value without using a secret."
														type:        "string"
													}
												}
												type: "object"
											}
										}
										required: [
											"accessKey",
											"projectId",
											"region",
											"secretKey",
										]
										type: "object"
									}
									senhasegura: {
										description: "Senhasegura configures this store to sync secrets using senhasegura provider"
										properties: {
											auth: {
												description: "Auth defines parameters to authenticate in senhasegura"
												properties: {
													clientId: type: "string"
													clientSecretSecretRef: {
														description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
												}
												required: [
													"clientId",
													"clientSecretSecretRef",
												]
												type: "object"
											}
											ignoreSslCertificate: {
												default:     false
												description: "IgnoreSslCertificate defines if SSL certificate must be ignored"
												type:        "boolean"
											}
											module: {
												description: "Module defines which senhasegura module should be used to get secrets"
												type:        "string"
											}
											url: {
												description: "URL of senhasegura"
												type:        "string"
											}
										}
										required: [
											"auth",
											"module",
											"url",
										]
										type: "object"
									}
									vault: {
										description: "Vault configures this store to sync secrets using Hashi provider"
										properties: {
											auth: {
												description: "Auth configures how secret-manager authenticates with the Vault server."
												properties: {
													appRole: {
														description: "AppRole authenticates with Vault using the App Role auth mechanism, with the role and secret stored in a Kubernetes Secret resource."
														properties: {
															path: {
																default:     "approle"
																description: "Path where the App Role authentication backend is mounted in Vault, e.g: \"approle\""
																type:        "string"
															}
															roleId: {
																description: "RoleID configured in the App Role authentication backend when setting up the authentication backend in Vault."
																type:        "string"
															}
															roleRef: {
																description: "Reference to a key in a Secret that contains the App Role ID used to authenticate with Vault. The `key` field must be specified and denotes which entry within the Secret resource is used as the app role id."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															secretRef: {
																description: "Reference to a key in a Secret that contains the App Role secret used to authenticate with Vault. The `key` field must be specified and denotes which entry within the Secret resource is used as the app role secret."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														required: [
															"path",
															"secretRef",
														]
														type: "object"
													}
													cert: {
														description: "Cert authenticates with TLS Certificates by passing client certificate, private key and ca certificate Cert authentication method"
														properties: {
															clientCert: {
																description: "ClientCert is a certificate to authenticate using the Cert Vault authentication method"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															secretRef: {
																description: "SecretRef to a key in a Secret resource containing client private key to authenticate with Vault using the Cert authentication method"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														type: "object"
													}
													iam: {
														description: "Iam authenticates with vault by passing a special AWS request signed with AWS IAM credentials AWS IAM authentication method"
														properties: {
															externalID: {
																description: "AWS External ID set on assumed IAM roles"
																type:        "string"
															}
															jwt: {
																description: "Specify a service account with IRSA enabled"
																properties: serviceAccountRef: {
																	description: "A reference to a ServiceAccount resource."
																	properties: {
																		audiences: {
																			description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
																			items: type: "string"
																			type: "array"
																		}
																		name: {
																			description: "The name of the ServiceAccount resource being referred to."
																			type:        "string"
																		}
																		namespace: {
																			description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																			type:        "string"
																		}
																	}
																	required: ["name"]
																	type: "object"
																}
																type: "object"
															}
															path: {
																description: "Path where the AWS auth method is enabled in Vault, e.g: \"aws\""
																type:        "string"
															}
															region: {
																description: "AWS region"
																type:        "string"
															}
															role: {
																description: "This is the AWS role to be assumed before talking to vault"
																type:        "string"
															}
															secretRef: {
																description: "Specify credentials in a Secret object"
																properties: {
																	accessKeyIDSecretRef: {
																		description: "The AccessKeyID is used for authentication"
																		properties: {
																			key: {
																				description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																				type:        "string"
																			}
																			name: {
																				description: "The name of the Secret resource being referred to."
																				type:        "string"
																			}
																			namespace: {
																				description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																				type:        "string"
																			}
																		}
																		type: "object"
																	}
																	secretAccessKeySecretRef: {
																		description: "The SecretAccessKey is used for authentication"
																		properties: {
																			key: {
																				description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																				type:        "string"
																			}
																			name: {
																				description: "The name of the Secret resource being referred to."
																				type:        "string"
																			}
																			namespace: {
																				description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																				type:        "string"
																			}
																		}
																		type: "object"
																	}
																	sessionTokenSecretRef: {
																		description: "The SessionToken used for authentication This must be defined if AccessKeyID and SecretAccessKey are temporary credentials see: https://docs.aws.amazon.com/IAM/latest/UserGuide/id_credentials_temp_use-resources.html"
																		properties: {
																			key: {
																				description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																				type:        "string"
																			}
																			name: {
																				description: "The name of the Secret resource being referred to."
																				type:        "string"
																			}
																			namespace: {
																				description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																				type:        "string"
																			}
																		}
																		type: "object"
																	}
																}
																type: "object"
															}
															vaultAwsIamServerID: {
																description: "X-Vault-AWS-IAM-Server-ID is an additional header used by Vault IAM auth method to mitigate against different types of replay attacks. More details here: https://developer.hashicorp.com/vault/docs/auth/aws"
																type:        "string"
															}
															vaultRole: {
																description: "Vault Role. In vault, a role describes an identity with a set of permissions, groups, or policies you want to attach a user of the secrets engine"
																type:        "string"
															}
														}
														required: ["vaultRole"]
														type: "object"
													}
													jwt: {
														description: "Jwt authenticates with Vault by passing role and JWT token using the JWT/OIDC authentication method"
														properties: {
															kubernetesServiceAccountToken: {
																description: "Optional ServiceAccountToken specifies the Kubernetes service account for which to request a token for with the `TokenRequest` API."
																properties: {
																	audiences: {
																		description: "Optional audiences field that will be used to request a temporary Kubernetes service account token for the service account referenced by `serviceAccountRef`. Defaults to a single audience `vault` it not specified. Deprecated: use serviceAccountRef.Audiences instead"
																		items: type: "string"
																		type: "array"
																	}
																	expirationSeconds: {
																		description: "Optional expiration time in seconds that will be used to request a temporary Kubernetes service account token for the service account referenced by `serviceAccountRef`. Deprecated: this will be removed in the future. Defaults to 10 minutes."
																		format:      "int64"
																		type:        "integer"
																	}
																	serviceAccountRef: {
																		description: "Service account field containing the name of a kubernetes ServiceAccount."
																		properties: {
																			audiences: {
																				description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
																				items: type: "string"
																				type: "array"
																			}
																			name: {
																				description: "The name of the ServiceAccount resource being referred to."
																				type:        "string"
																			}
																			namespace: {
																				description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																				type:        "string"
																			}
																		}
																		required: ["name"]
																		type: "object"
																	}
																}
																required: ["serviceAccountRef"]
																type: "object"
															}
															path: {
																default:     "jwt"
																description: "Path where the JWT authentication backend is mounted in Vault, e.g: \"jwt\""
																type:        "string"
															}
															role: {
																description: "Role is a JWT role to authenticate using the JWT/OIDC Vault authentication method"
																type:        "string"
															}
															secretRef: {
																description: "Optional SecretRef that refers to a key in a Secret resource containing JWT token to authenticate with Vault using the JWT/OIDC authentication method."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														required: ["path"]
														type: "object"
													}
													kubernetes: {
														description: "Kubernetes authenticates with Vault by passing the ServiceAccount token stored in the named Secret resource to the Vault server."
														properties: {
															mountPath: {
																default:     "kubernetes"
																description: "Path where the Kubernetes authentication backend is mounted in Vault, e.g: \"kubernetes\""
																type:        "string"
															}
															role: {
																description: "A required field containing the Vault Role to assume. A Role binds a Kubernetes ServiceAccount with a set of Vault policies."
																type:        "string"
															}
															secretRef: {
																description: "Optional secret field containing a Kubernetes ServiceAccount JWT used for authenticating with Vault. If a name is specified without a key, `token` is the default. If one is not specified, the one bound to the controller will be used."
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															serviceAccountRef: {
																description: "Optional service account field containing the name of a kubernetes ServiceAccount. If the service account is specified, the service account secret token JWT will be used for authenticating with Vault. If the service account selector is not supplied, the secretRef will be used instead."
																properties: {
																	audiences: {
																		description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
																		items: type: "string"
																		type: "array"
																	}
																	name: {
																		description: "The name of the ServiceAccount resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																required: ["name"]
																type: "object"
															}
														}
														required: [
															"mountPath",
															"role",
														]
														type: "object"
													}
													ldap: {
														description: "Ldap authenticates with Vault by passing username/password pair using the LDAP authentication method"
														properties: {
															path: {
																default:     "ldap"
																description: "Path where the LDAP authentication backend is mounted in Vault, e.g: \"ldap\""
																type:        "string"
															}
															secretRef: {
																description: "SecretRef to a key in a Secret resource containing password for the LDAP user used to authenticate with Vault using the LDAP authentication method"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															username: {
																description: "Username is a LDAP user name used to authenticate using the LDAP Vault authentication method"
																type:        "string"
															}
														}
														required: [
															"path",
															"username",
														]
														type: "object"
													}
													tokenSecretRef: {
														description: "TokenSecretRef authenticates with Vault by presenting a token."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
													userPass: {
														description: "UserPass authenticates with Vault by passing username/password pair"
														properties: {
															path: {
																default:     "user"
																description: "Path where the UserPassword authentication backend is mounted in Vault, e.g: \"user\""
																type:        "string"
															}
															secretRef: {
																description: "SecretRef to a key in a Secret resource containing password for the user used to authenticate with Vault using the UserPass authentication method"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															username: {
																description: "Username is a user name used to authenticate using the UserPass Vault authentication method"
																type:        "string"
															}
														}
														required: [
															"path",
															"username",
														]
														type: "object"
													}
												}
												type: "object"
											}
											caBundle: {
												description: "PEM encoded CA bundle used to validate Vault server certificate. Only used if the Server URL is using HTTPS protocol. This parameter is ignored for plain HTTP protocol connection. If not set the system root certificates are used to validate the TLS connection."
												format:      "byte"
												type:        "string"
											}
											caProvider: {
												description: "The provider for the CA bundle to use to validate Vault server certificate."
												properties: {
													key: {
														description: "The key where the CA certificate can be found in the Secret or ConfigMap."
														type:        "string"
													}
													name: {
														description: "The name of the object located at the provider type."
														type:        "string"
													}
													namespace: {
														description: "The namespace the Provider type is in. Can only be defined when used in a ClusterSecretStore."
														type:        "string"
													}
													type: {
														description: "The type of provider to use such as \"Secret\", or \"ConfigMap\"."
														enum: [
															"Secret",
															"ConfigMap",
														]
														type: "string"
													}
												}
												required: [
													"name",
													"type",
												]
												type: "object"
											}
											forwardInconsistent: {
												description: "ForwardInconsistent tells Vault to forward read-after-write requests to the Vault leader instead of simply retrying within a loop. This can increase performance if the option is enabled serverside. https://www.vaultproject.io/docs/configuration/replication#allow_forwarding_via_header"
												type:        "boolean"
											}
											namespace: {
												description: "Name of the vault namespace. Namespaces is a set of features within Vault Enterprise that allows Vault environments to support Secure Multi-tenancy. e.g: \"ns1\". More about namespaces can be found here https://www.vaultproject.io/docs/enterprise/namespaces"
												type:        "string"
											}
											path: {
												description: "Path is the mount path of the Vault KV backend endpoint, e.g: \"secret\". The v2 KV secret engine version specific \"/data\" path suffix for fetching secrets from Vault is optional and will be appended if not present in specified path."
												type:        "string"
											}
											readYourWrites: {
												description: "ReadYourWrites ensures isolated read-after-write semantics by providing discovered cluster replication states in each request. More information about eventual consistency in Vault can be found here https://www.vaultproject.io/docs/enterprise/consistency"
												type:        "boolean"
											}
											server: {
												description: "Server is the connection address for the Vault server, e.g: \"https://vault.example.com:8200\"."
												type:        "string"
											}
											version: {
												default:     "v2"
												description: "Version is the Vault KV secret engine version. This can be either \"v1\" or \"v2\". Version defaults to \"v2\"."
												enum: [
													"v1",
													"v2",
												]
												type: "string"
											}
										}
										required: [
											"auth",
											"server",
										]
										type: "object"
									}
									webhook: {
										description: "Webhook configures this store to sync secrets using a generic templated webhook"
										properties: {
											body: {
												description: "Body"
												type:        "string"
											}
											caBundle: {
												description: "PEM encoded CA bundle used to validate webhook server certificate. Only used if the Server URL is using HTTPS protocol. This parameter is ignored for plain HTTP protocol connection. If not set the system root certificates are used to validate the TLS connection."
												format:      "byte"
												type:        "string"
											}
											caProvider: {
												description: "The provider for the CA bundle to use to validate webhook server certificate."
												properties: {
													key: {
														description: "The key the value inside of the provider type to use, only used with \"Secret\" type"
														type:        "string"
													}
													name: {
														description: "The name of the object located at the provider type."
														type:        "string"
													}
													namespace: {
														description: "The namespace the Provider type is in."
														type:        "string"
													}
													type: {
														description: "The type of provider to use such as \"Secret\", or \"ConfigMap\"."
														enum: [
															"Secret",
															"ConfigMap",
														]
														type: "string"
													}
												}
												required: [
													"name",
													"type",
												]
												type: "object"
											}
											headers: {
												additionalProperties: type: "string"
												description: "Headers"
												type:        "object"
											}
											method: {
												description: "Webhook Method"
												type:        "string"
											}
											result: {
												description: "Result formatting"
												properties: jsonPath: {
													description: "Json path of return value"
													type:        "string"
												}
												type: "object"
											}
											secrets: {
												description: "Secrets to fill in templates These secrets will be passed to the templating function as key value pairs under the given name"
												items: {
													properties: {
														name: {
															description: "Name of this secret in templates"
															type:        "string"
														}
														secretRef: {
															description: "Secret ref to fill in credentials"
															properties: {
																key: {
																	description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																	type:        "string"
																}
																name: {
																	description: "The name of the Secret resource being referred to."
																	type:        "string"
																}
																namespace: {
																	description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																	type:        "string"
																}
															}
															type: "object"
														}
													}
													required: [
														"name",
														"secretRef",
													]
													type: "object"
												}
												type: "array"
											}
											timeout: {
												description: "Timeout"
												type:        "string"
											}
											url: {
												description: "Webhook url to call"
												type:        "string"
											}
										}
										required: [
											"result",
											"url",
										]
										type: "object"
									}
									yandexcertificatemanager: {
										description: "YandexCertificateManager configures this store to sync secrets using Yandex Certificate Manager provider"
										properties: {
											apiEndpoint: {
												description: "Yandex.Cloud API endpoint (e.g. 'api.cloud.yandex.net:443')"
												type:        "string"
											}
											auth: {
												description: "Auth defines the information necessary to authenticate against Yandex Certificate Manager"
												properties: authorizedKeySecretRef: {
													description: "The authorized key used for authentication"
													properties: {
														key: {
															description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
															type:        "string"
														}
														name: {
															description: "The name of the Secret resource being referred to."
															type:        "string"
														}
														namespace: {
															description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
															type:        "string"
														}
													}
													type: "object"
												}
												type: "object"
											}
											caProvider: {
												description: "The provider for the CA bundle to use to validate Yandex.Cloud server certificate."
												properties: certSecretRef: {
													description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
													properties: {
														key: {
															description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
															type:        "string"
														}
														name: {
															description: "The name of the Secret resource being referred to."
															type:        "string"
														}
														namespace: {
															description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
															type:        "string"
														}
													}
													type: "object"
												}
												type: "object"
											}
										}
										required: ["auth"]
										type: "object"
									}
									yandexlockbox: {
										description: "YandexLockbox configures this store to sync secrets using Yandex Lockbox provider"
										properties: {
											apiEndpoint: {
												description: "Yandex.Cloud API endpoint (e.g. 'api.cloud.yandex.net:443')"
												type:        "string"
											}
											auth: {
												description: "Auth defines the information necessary to authenticate against Yandex Lockbox"
												properties: authorizedKeySecretRef: {
													description: "The authorized key used for authentication"
													properties: {
														key: {
															description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
															type:        "string"
														}
														name: {
															description: "The name of the Secret resource being referred to."
															type:        "string"
														}
														namespace: {
															description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
															type:        "string"
														}
													}
													type: "object"
												}
												type: "object"
											}
											caProvider: {
												description: "The provider for the CA bundle to use to validate Yandex.Cloud server certificate."
												properties: certSecretRef: {
													description: "A reference to a specific 'key' within a Secret resource, In some instances, `key` is a required field."
													properties: {
														key: {
															description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
															type:        "string"
														}
														name: {
															description: "The name of the Secret resource being referred to."
															type:        "string"
														}
														namespace: {
															description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
															type:        "string"
														}
													}
													type: "object"
												}
												type: "object"
											}
										}
										required: ["auth"]
										type: "object"
									}
								}
								type: "object"
							}
							refreshInterval: {
								description: "Used to configure store refresh interval in seconds. Empty or 0 will default to the controller config."
								type:        "integer"
							}
							retrySettings: {
								description: "Used to configure http retries if failed"
								properties: {
									maxRetries: {
										format: "int32"
										type:   "integer"
									}
									retryInterval: type: "string"
								}
								type: "object"
							}
						}
						required: ["provider"]
						type: "object"
					}
					status: {
						description: "SecretStoreStatus defines the observed state of the SecretStore."
						properties: {
							capabilities: {
								description: "SecretStoreCapabilities defines the possible operations a SecretStore can do."
								type:        "string"
							}
							conditions: {
								items: {
									properties: {
										lastTransitionTime: {
											format: "date-time"
											type:   "string"
										}
										message: type: "string"
										reason: type:  "string"
										status: type:  "string"
										type: type:    "string"
									}
									required: [
										"status",
										"type",
									]
									type: "object"
								}
								type: "array"
							}
						}
						type: "object"
					}
				}
				type: "object"
			}
			served:  true
			storage: true
			subresources: status: {}
		}]
		conversion: {
			strategy: "Webhook"
			webhook: {
				conversionReviewVersions: ["v1"]
				clientConfig: service: {
					name:      "external-secrets-webhook"
					namespace: "external-secrets"
					path:      "/convert"
				}
			}
		}
	}
}, {
	spec: {
		group: "generators.external-secrets.io"
		names: {
			categories: ["vaultdynamicsecret"]
			kind:     "VaultDynamicSecret"
			listKind: "VaultDynamicSecretList"
			plural:   "vaultdynamicsecrets"
			shortNames: ["vaultdynamicsecret"]
			singular: "vaultdynamicsecret"
		}
		scope: "Namespaced"
		versions: [{
			name: "v1alpha1"
			schema: openAPIV3Schema: {
				properties: {
					apiVersion: {
						description: "APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources"
						type:        "string"
					}
					kind: {
						description: "Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds"
						type:        "string"
					}
					metadata: type: "object"
					spec: {
						properties: {
							controller: {
								description: "Used to select the correct ESO controller (think: ingress.ingressClassName) The ESO controller is instantiated with a specific controller name and filters VDS based on this property"
								type:        "string"
							}
							method: {
								description: "Vault API method to use (GET/POST/other)"
								type:        "string"
							}
							parameters: {
								description:                            "Parameters to pass to Vault write (for non-GET methods)"
								"x-kubernetes-preserve-unknown-fields": true
							}
							path: {
								description: "Vault path to obtain the dynamic secret from"
								type:        "string"
							}
							provider: {
								description: "Vault provider common spec"
								properties: {
									auth: {
										description: "Auth configures how secret-manager authenticates with the Vault server."
										properties: {
											appRole: {
												description: "AppRole authenticates with Vault using the App Role auth mechanism, with the role and secret stored in a Kubernetes Secret resource."
												properties: {
													path: {
														default:     "approle"
														description: "Path where the App Role authentication backend is mounted in Vault, e.g: \"approle\""
														type:        "string"
													}
													roleId: {
														description: "RoleID configured in the App Role authentication backend when setting up the authentication backend in Vault."
														type:        "string"
													}
													roleRef: {
														description: "Reference to a key in a Secret that contains the App Role ID used to authenticate with Vault. The `key` field must be specified and denotes which entry within the Secret resource is used as the app role id."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
													secretRef: {
														description: "Reference to a key in a Secret that contains the App Role secret used to authenticate with Vault. The `key` field must be specified and denotes which entry within the Secret resource is used as the app role secret."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
												}
												required: [
													"path",
													"secretRef",
												]
												type: "object"
											}
											cert: {
												description: "Cert authenticates with TLS Certificates by passing client certificate, private key and ca certificate Cert authentication method"
												properties: {
													clientCert: {
														description: "ClientCert is a certificate to authenticate using the Cert Vault authentication method"
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
													secretRef: {
														description: "SecretRef to a key in a Secret resource containing client private key to authenticate with Vault using the Cert authentication method"
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
												}
												type: "object"
											}
											iam: {
												description: "Iam authenticates with vault by passing a special AWS request signed with AWS IAM credentials AWS IAM authentication method"
												properties: {
													externalID: {
														description: "AWS External ID set on assumed IAM roles"
														type:        "string"
													}
													jwt: {
														description: "Specify a service account with IRSA enabled"
														properties: serviceAccountRef: {
															description: "A reference to a ServiceAccount resource."
															properties: {
																audiences: {
																	description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
																	items: type: "string"
																	type: "array"
																}
																name: {
																	description: "The name of the ServiceAccount resource being referred to."
																	type:        "string"
																}
																namespace: {
																	description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																	type:        "string"
																}
															}
															required: ["name"]
															type: "object"
														}
														type: "object"
													}
													path: {
														description: "Path where the AWS auth method is enabled in Vault, e.g: \"aws\""
														type:        "string"
													}
													region: {
														description: "AWS region"
														type:        "string"
													}
													role: {
														description: "This is the AWS role to be assumed before talking to vault"
														type:        "string"
													}
													secretRef: {
														description: "Specify credentials in a Secret object"
														properties: {
															accessKeyIDSecretRef: {
																description: "The AccessKeyID is used for authentication"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															secretAccessKeySecretRef: {
																description: "The SecretAccessKey is used for authentication"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
															sessionTokenSecretRef: {
																description: "The SessionToken used for authentication This must be defined if AccessKeyID and SecretAccessKey are temporary credentials see: https://docs.aws.amazon.com/IAM/latest/UserGuide/id_credentials_temp_use-resources.html"
																properties: {
																	key: {
																		description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																		type:        "string"
																	}
																	name: {
																		description: "The name of the Secret resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																type: "object"
															}
														}
														type: "object"
													}
													vaultAwsIamServerID: {
														description: "X-Vault-AWS-IAM-Server-ID is an additional header used by Vault IAM auth method to mitigate against different types of replay attacks. More details here: https://developer.hashicorp.com/vault/docs/auth/aws"
														type:        "string"
													}
													vaultRole: {
														description: "Vault Role. In vault, a role describes an identity with a set of permissions, groups, or policies you want to attach a user of the secrets engine"
														type:        "string"
													}
												}
												required: ["vaultRole"]
												type: "object"
											}
											jwt: {
												description: "Jwt authenticates with Vault by passing role and JWT token using the JWT/OIDC authentication method"
												properties: {
													kubernetesServiceAccountToken: {
														description: "Optional ServiceAccountToken specifies the Kubernetes service account for which to request a token for with the `TokenRequest` API."
														properties: {
															audiences: {
																description: "Optional audiences field that will be used to request a temporary Kubernetes service account token for the service account referenced by `serviceAccountRef`. Defaults to a single audience `vault` it not specified. Deprecated: use serviceAccountRef.Audiences instead"
																items: type: "string"
																type: "array"
															}
															expirationSeconds: {
																description: "Optional expiration time in seconds that will be used to request a temporary Kubernetes service account token for the service account referenced by `serviceAccountRef`. Deprecated: this will be removed in the future. Defaults to 10 minutes."
																format:      "int64"
																type:        "integer"
															}
															serviceAccountRef: {
																description: "Service account field containing the name of a kubernetes ServiceAccount."
																properties: {
																	audiences: {
																		description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
																		items: type: "string"
																		type: "array"
																	}
																	name: {
																		description: "The name of the ServiceAccount resource being referred to."
																		type:        "string"
																	}
																	namespace: {
																		description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																		type:        "string"
																	}
																}
																required: ["name"]
																type: "object"
															}
														}
														required: ["serviceAccountRef"]
														type: "object"
													}
													path: {
														default:     "jwt"
														description: "Path where the JWT authentication backend is mounted in Vault, e.g: \"jwt\""
														type:        "string"
													}
													role: {
														description: "Role is a JWT role to authenticate using the JWT/OIDC Vault authentication method"
														type:        "string"
													}
													secretRef: {
														description: "Optional SecretRef that refers to a key in a Secret resource containing JWT token to authenticate with Vault using the JWT/OIDC authentication method."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
												}
												required: ["path"]
												type: "object"
											}
											kubernetes: {
												description: "Kubernetes authenticates with Vault by passing the ServiceAccount token stored in the named Secret resource to the Vault server."
												properties: {
													mountPath: {
														default:     "kubernetes"
														description: "Path where the Kubernetes authentication backend is mounted in Vault, e.g: \"kubernetes\""
														type:        "string"
													}
													role: {
														description: "A required field containing the Vault Role to assume. A Role binds a Kubernetes ServiceAccount with a set of Vault policies."
														type:        "string"
													}
													secretRef: {
														description: "Optional secret field containing a Kubernetes ServiceAccount JWT used for authenticating with Vault. If a name is specified without a key, `token` is the default. If one is not specified, the one bound to the controller will be used."
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
													serviceAccountRef: {
														description: "Optional service account field containing the name of a kubernetes ServiceAccount. If the service account is specified, the service account secret token JWT will be used for authenticating with Vault. If the service account selector is not supplied, the secretRef will be used instead."
														properties: {
															audiences: {
																description: "Audience specifies the `aud` claim for the service account token If the service account uses a well-known annotation for e.g. IRSA or GCP Workload Identity then this audiences will be appended to the list"
																items: type: "string"
																type: "array"
															}
															name: {
																description: "The name of the ServiceAccount resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														required: ["name"]
														type: "object"
													}
												}
												required: [
													"mountPath",
													"role",
												]
												type: "object"
											}
											ldap: {
												description: "Ldap authenticates with Vault by passing username/password pair using the LDAP authentication method"
												properties: {
													path: {
														default:     "ldap"
														description: "Path where the LDAP authentication backend is mounted in Vault, e.g: \"ldap\""
														type:        "string"
													}
													secretRef: {
														description: "SecretRef to a key in a Secret resource containing password for the LDAP user used to authenticate with Vault using the LDAP authentication method"
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
													username: {
														description: "Username is a LDAP user name used to authenticate using the LDAP Vault authentication method"
														type:        "string"
													}
												}
												required: [
													"path",
													"username",
												]
												type: "object"
											}
											tokenSecretRef: {
												description: "TokenSecretRef authenticates with Vault by presenting a token."
												properties: {
													key: {
														description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
														type:        "string"
													}
													name: {
														description: "The name of the Secret resource being referred to."
														type:        "string"
													}
													namespace: {
														description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
														type:        "string"
													}
												}
												type: "object"
											}
											userPass: {
												description: "UserPass authenticates with Vault by passing username/password pair"
												properties: {
													path: {
														default:     "user"
														description: "Path where the UserPassword authentication backend is mounted in Vault, e.g: \"user\""
														type:        "string"
													}
													secretRef: {
														description: "SecretRef to a key in a Secret resource containing password for the user used to authenticate with Vault using the UserPass authentication method"
														properties: {
															key: {
																description: "The key of the entry in the Secret resource's `data` field to be used. Some instances of this field may be defaulted, in others it may be required."
																type:        "string"
															}
															name: {
																description: "The name of the Secret resource being referred to."
																type:        "string"
															}
															namespace: {
																description: "Namespace of the resource being referred to. Ignored if referent is not cluster-scoped. cluster-scoped defaults to the namespace of the referent."
																type:        "string"
															}
														}
														type: "object"
													}
													username: {
														description: "Username is a user name used to authenticate using the UserPass Vault authentication method"
														type:        "string"
													}
												}
												required: [
													"path",
													"username",
												]
												type: "object"
											}
										}
										type: "object"
									}
									caBundle: {
										description: "PEM encoded CA bundle used to validate Vault server certificate. Only used if the Server URL is using HTTPS protocol. This parameter is ignored for plain HTTP protocol connection. If not set the system root certificates are used to validate the TLS connection."
										format:      "byte"
										type:        "string"
									}
									caProvider: {
										description: "The provider for the CA bundle to use to validate Vault server certificate."
										properties: {
											key: {
												description: "The key where the CA certificate can be found in the Secret or ConfigMap."
												type:        "string"
											}
											name: {
												description: "The name of the object located at the provider type."
												type:        "string"
											}
											namespace: {
												description: "The namespace the Provider type is in. Can only be defined when used in a ClusterSecretStore."
												type:        "string"
											}
											type: {
												description: "The type of provider to use such as \"Secret\", or \"ConfigMap\"."
												enum: [
													"Secret",
													"ConfigMap",
												]
												type: "string"
											}
										}
										required: [
											"name",
											"type",
										]
										type: "object"
									}
									forwardInconsistent: {
										description: "ForwardInconsistent tells Vault to forward read-after-write requests to the Vault leader instead of simply retrying within a loop. This can increase performance if the option is enabled serverside. https://www.vaultproject.io/docs/configuration/replication#allow_forwarding_via_header"
										type:        "boolean"
									}
									namespace: {
										description: "Name of the vault namespace. Namespaces is a set of features within Vault Enterprise that allows Vault environments to support Secure Multi-tenancy. e.g: \"ns1\". More about namespaces can be found here https://www.vaultproject.io/docs/enterprise/namespaces"
										type:        "string"
									}
									path: {
										description: "Path is the mount path of the Vault KV backend endpoint, e.g: \"secret\". The v2 KV secret engine version specific \"/data\" path suffix for fetching secrets from Vault is optional and will be appended if not present in specified path."
										type:        "string"
									}
									readYourWrites: {
										description: "ReadYourWrites ensures isolated read-after-write semantics by providing discovered cluster replication states in each request. More information about eventual consistency in Vault can be found here https://www.vaultproject.io/docs/enterprise/consistency"
										type:        "boolean"
									}
									server: {
										description: "Server is the connection address for the Vault server, e.g: \"https://vault.example.com:8200\"."
										type:        "string"
									}
									version: {
										default:     "v2"
										description: "Version is the Vault KV secret engine version. This can be either \"v1\" or \"v2\". Version defaults to \"v2\"."
										enum: [
											"v1",
											"v2",
										]
										type: "string"
									}
								}
								required: [
									"auth",
									"server",
								]
								type: "object"
							}
							resultType: {
								default:     "Data"
								description: "Result type defines which data is returned from the generator. By default it is the \"data\" section of the Vault API response. When using e.g. /auth/token/create the \"data\" section is empty but the \"auth\" section contains the generated token. Please refer to the vault docs regarding the result data structure."
								type:        "string"
							}
						}
						required: [
							"path",
							"provider",
						]
						type: "object"
					}
				}
				type: "object"
			}
			served:  true
			storage: true
			subresources: status: {}
		}]
		conversion: {
			strategy: "Webhook"
			webhook: {
				conversionReviewVersions: ["v1"]
				clientConfig: service: {
					name:      "external-secrets-webhook"
					namespace: "external-secrets"
					path:      "/convert"
				}
			}
		}
	}
}]

