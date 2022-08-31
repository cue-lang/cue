info: {
  title: "Foo API"
  version: "v1"
}

#Foo: bar?: number

"$/foo":  post: {
  description: "foo it"
  responses: "200": content: "application/json": schema: #Foo
}