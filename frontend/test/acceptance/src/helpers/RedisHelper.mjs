import RedisWrapper from '../../../../../services/web/app/src/infrastructure/RedisWrapper.mjs'
const client = RedisWrapper.client('ratelimiter')

export default {
  initialize() {
    beforeEach('clear redis', function (done) {
      client.flushdb(done)
    })
  },
}
