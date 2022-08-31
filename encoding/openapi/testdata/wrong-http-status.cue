"$/ping": {
  security: ["token", "user"]
  description: "Ping endpoint"
  get: {
      description: "Returns pong"
      responses:{
        '666':{
          content: {
            "application/json":{
              schema: {}
            }              
          }
        }
      }
  }
}
