"$/ping": {
  security: [{
      "type": ["http"],
      "scheme": ["basic"]
    }]
  description: "Ping endpoint"
  get: {
      description: "Returns pong"
      responses:{
        '200':{
          content: {
            "text/plain":{
              schema: string

            }              
          }
        }
      }
  }
  post: {
      description: "Received a pong"
      responses:{
        '200':{
          content: {
            "application/json":{
              schema: int
            }              
          }
        }
      }
  }


}
