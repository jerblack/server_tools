
const reportAll = async () => {
    if (await db.isUserGatherQueueEmpty()) {
        await db.loadUserGatherQueue()
    }
    await pager.isQueueActive()
}

const pager = {
    numTweets: 30,
    currentUser: undefined,
    isQueueActive: async () => {
        if (abort()) {return}
        const userGatherEmpty = await db.isUserGatherQueueEmpty()
        if (!userGatherEmpty) {
            console.log("user gather not empty")
            const user = await pager.loadNextUserPage()
            pager.currentUser = user
            console.log("current page is user page for " + user.user)
            await sleep(4000)
            await pager.hideHeader()
            await pager.tweetGather(user)
            await db.deleteFromUserGatherQueue(user)
            await pager.loadNextUserPage()
        }
        const tweetQueueEmpty = await db.isTweetQueueEmpty()
        if (!tweetQueueEmpty) {
            const id = getIdFromUri()
            if (id && await db.inTweetQueue(id)) {
                let ok = await report.start()
                if (!ok) {return}
                await db.deleteFromTweetQueue(id)
            }
            const tw = await db.getNextFromTweetQueue()
            if (tw) {
                window.location.href = `https://twitter.com/${tw.user}/status/${tw.tweetId}`
            }
        }
        console.log("tweet queue empty")
    },
    loadNextUserPage: async () => {
        const currentUser = getUserFromUserPageURI()
        if (currentUser) {
            const user = await db.getFromGatherQueueByName(currentUser)
            console.log("lnup user: "+ user.user)
            console.log("lnup currentUser: " + currentUser)
            if (user) {
                console.log("lnup match return")
                return user
            }
        }

        const nextUser = await  db.getNextFromUserGatherQueue()
        if (nextUser) {
            console.log("lnup users length > 0, loading to next page")
            window.location.href = `https://twitter.com/${nextUser.user}/with_replies`
            return nextUser
        }
    },
    hideHeader: () => {
        const hdr = document.querySelector("div.css-1dbjc4n.r-1jgb5lz.r-1ye8kvj.r-13qz1uu div.css-1dbjc4n.r-1jgb5lz.r-1ye8kvj.r-13qz1uu > div.css-1dbjc4n")
        hdr.style.display = "none"
    },
    tweetGather: async user => {
        let totalCount = 0
        let count = -1
        const doList = async () => {
            const list = document.querySelector("section[aria-labelledby^='accessible-list-'] > div > div")
            count = 0
            for (const elm of list.childNodes) {
                if (elm.style.display === "none") {
                    continue
                }

                if (elm.textContent === "") {
                    await sleep(400)
                    console.log("collapsing unrelated ui")
                    elm.style.display = "none"
                    continue
                }

                const twUser = getUserFromTweet(elm)
                if (twUser === user.user) {
                    const twId = getIdFromTweet(elm)
                    await db.addToTweetQueue(twId, twUser)
                    elm.scrollIntoView()
                    elm.style.display = "none"
                    totalCount++
                    count++
                    console.log(`logging tweet ${totalCount} with id ${twId} | count now ${count}`)
                    if (totalCount >= user.numTweets) {
                        return
                    }
                    await sleep(400)
                    continue
                }
                await sleep(400)
                console.log("collapsing unrelated tweet")
                elm.style.display = "none"
            }
        }
        while (count !== 0 && totalCount <= user.numTweets) {
            await sleep(2000)

            await doList()
        }

    }
}

