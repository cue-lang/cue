"$/ping": {
  security: ["token", "user"]
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
        '400': {
            content: {
                "text/plain": {
                    schema: string
                }
            }
        }
      }
  }
}
