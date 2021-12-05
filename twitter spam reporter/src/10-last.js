window.addEventListener("load", async () => {
    await db.open()
    await pager.isQueueActive()
})

const setup = async () => {
    await Promise.all([
        db.preloadKeywords(),
        db.addMonitoredUser("elonmusk"),
        db.addMonitoredUser("spacex"),
        db.addMonitoredUser("tesla")
    ])
}

const addKeywords = (...terms) => {
    for (let term of terms) {
        db.addKeyword(term).then(ifOut)
    }
}
const addKeyword = addKeywords

const addRegex = re => {
    db.addRegex(re).then(ifOut)
}
const addPattern = addRegex
const stop = () => {
    report.stop = true
}
const addSpammer = user => {
    db.addReportedUser(user).then(ifOut)
}
const clear = () => {
    db.clearTweetQueue().then(ifOut)
}
const pause = () => {
    report.pause = true
}
const print = n => {
    db.printUnreportedTweets(n).then(ifOut)
}

const x = {
    print: print,
    stop: stop,
    pause: pause,
    addRegex: addRegex,
    addKeyword: addKeyword,
    addKeywords: addKeywords,
    clear: clear,
    addSpammer: addSpammer
}
