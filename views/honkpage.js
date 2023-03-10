function encode(hash) {
        var s = []
        for (var key in hash) {
                var val = hash[key]
                s.push(escape(key) + "=" + escape(val))
        }
        return s.join("&")
}
function post(url, data) {
	var x = new XMLHttpRequest()
	x.open("POST", url)
	x.timeout = 30 * 1000
	x.setRequestHeader("Content-Type", "application/x-www-form-urlencoded")
	x.send(data)
}
function get(url, whendone, whentimedout) {
	var x = new XMLHttpRequest()
	x.open("GET", url)
	x.timeout = 15 * 1000
	x.responseType = "json"
	x.onload = function() { whendone(x) }
	if (whentimedout) {
		x.ontimeout = function(e) { whentimedout(x, e) }
	}
	x.send()
}
function share(el, xid) {
	el.innerHTML = "shared"
	el.disabled = true
	post("/share", encode({"js": "2", "CSRF": csrftoken, "xid": xid}))
	return false
}
function unshare(el, xid) {
	el.innerHTML = "unshared"
	el.disabled = true
	post("/zonkit", encode({"CSRF": csrftoken, "action": "unshare", "what": xid}))
}
function muteit(el, thread) {
	el.innerHTML = "muted"
	el.disabled = true
	post("/zonkit", encode({"CSRF": csrftoken, "action": "mute-thread", "what": thread}))
	var els = document.querySelectorAll('article.honk')
	for (var i = 0; i < els.length; i++) {
		var e = els[i]
		if (e.getAttribute("data-thread") == thread) {
			e.remove()
		}
	}
}
function zonkit(el, xid) {
	el.innerHTML = "zonked"
	el.disabled = true
	post("/zonkit", encode({"CSRF": csrftoken, "action": "zonk", "what": xid}))
	var p = el
	while (p && p.tagName != "ARTICLE") {
		p = p.parentElement
	}
	if (p) {
		p.remove()
	}
}
function flogit(el, how, xid) {
	var s = how
	if (s[s.length-1] != "e") { s += "e" }
	s += "d"
	if (s == "untaged") s = "untagged"
	el.innerHTML = s
	el.disabled = true
	post("/zonkit", encode({"CSRF": csrftoken, "action": how, "what": xid}))
}

var lehonkform = document.getElementById("honkform")
var lehonkbutton = document.getElementById("honkingtime")

function oldestnewest(btn) {
	var els = document.getElementsByClassName("glow")
	if (els.length) {
		els[els.length-1].scrollIntoView()
	}
}
function removeglow() {
	var els = document.getElementsByClassName("glow")
	while (els.length) {
		els[0].classList.remove("glow")
	}
}

function fillinhonks(xhr, glowit) {
	var resp = xhr.response
	var stash = curpagestate.name + ":" + curpagestate.arg
	tophid[stash] = resp.Tophid
	var doc = document.createElement( 'div' );
	doc.innerHTML = resp.Srvmsg
	var srvmsg = doc
	doc = document.createElement( 'div' );
	doc.innerHTML = resp.Honks
	var honks = doc.children

	var mecount = document.getElementById("mecount")
	if (resp.MeCount) {
		mecount.innerHTML = "(" + resp.MeCount + ")"
	} else {
		mecount.innerHTML = ""
	}
	var chatcount = document.getElementById("chatcount")
	if (resp.ChatCount) {
		chatcount.innerHTML = "(" + resp.ChatCount + ")"
	} else {
		chatcount.innerHTML = ""
	}

	var srvel = document.getElementById("srvmsg")
	while (srvel.children[0]) {
		srvel.children[0].remove()
	}
	srvel.prepend(srvmsg)

	var frontload = true
	if (curpagestate.name == "thread") {
		frontload = false
	}

	var honksonpage = document.getElementById("honksonpage")
	var holder = honksonpage.children[0]
	var lenhonks = honks.length
	for (var i = honks.length; i > 0; i--) {
		var h = honks[i-1]
		if (glowit)
			h.classList.add("glow")
		if (frontload) {
			holder.prepend(h)
		} else {
			holder.append(h)
		}
	}
	relinklinks()
	return lenhonks
}
function hydrargs() {
	var name = curpagestate.name
	var arg = curpagestate.arg
	var args = { "page" : name }
	if (name == "thread") {
		args["c"] = arg
	} else if (name == "combo") {
		args["c"] = arg
	} else if (name == "author") {
		args["xid"] = arg
	} else if (name == "user") {
		args["uname"] = arg
	}
	return args
}
function refreshupdate(msg) {
	var el = document.querySelector("#refreshbox p span")
	if (el) {
		el.innerHTML = msg
	}
}
function refreshhonks(btn) {
	removeglow()
	btn.innerHTML = "refreshing"
	btn.disabled = true
	var args = hydrargs()
	var stash = curpagestate.name + ":" + curpagestate.arg
	args["tophid"] = tophid[stash]
	get("/hydra?" + encode(args), function(xhr) {
		btn.innerHTML = "refresh"
		btn.disabled = false
		if (xhr.status == 200) {
			var lenhonks = fillinhonks(xhr, true)
			refreshupdate(" " + lenhonks + " new")
		} else {
			refreshupdate(" status: " + xhr.status)
		}
	}, function(xhr, e) {
		btn.innerHTML = "refresh"
		btn.disabled = false
		refreshupdate(" timed out")
	})
}
function statechanger(evt) {
	var data = evt.state
	if (!data) {
		return
	}
	switchtopage(data.name, data.arg)
}
function switchtopage(name, arg) {
	var stash = curpagestate.name + ":" + curpagestate.arg
	var honksonpage = document.getElementById("honksonpage")
	var holder = honksonpage.children[0]
	holder.remove()
	var srvel = document.getElementById("srvmsg")
	var msg = srvel.children[0]
	if (msg) {
		msg.remove()
		servermsgs[stash] = msg
	}
	showelement("refreshbox")

	honksforpage[stash] = holder

	curpagestate.name = name
	curpagestate.arg = arg
	// get the holder for the target page
	var stash = name + ":" + arg
	holder = honksforpage[stash]
	if (holder) {
		honksonpage.prepend(holder)
		msg = servermsgs[stash]
		if (msg) {
			srvel.prepend(msg)
		}
	} else {
		// or create one and fill it
		honksonpage.prepend(document.createElement("div"))
		var args = hydrargs()
		get("/hydra?" + encode(args), function(xhr) {
			if (xhr.status == 200) {
				var lenhonks = fillinhonks(xhr, false)
			} else {
				refreshupdate(" status: " + xhr.status)
			}
		}, function(xhr, e) {
			refreshupdate(" timed out")
		})
	}
	refreshupdate("")
}
function newpagestate(name, arg) {
	return { "name": name, "arg": arg }
}
function pageswitcher(name, arg) {
	return function(evt) {
		var topmenu = document.getElementById("topmenu")
		topmenu.open = false
		if (name == curpagestate.name && arg == curpagestate.arg) {
			return false
		}
		switchtopage(name, arg)
		var url = evt.srcElement.href
		if (!url) {
			url = evt.srcElement.parentElement.href
		}
		history.pushState(newpagestate(name, arg), "some title", url)
		window.scrollTo(0, 0)
		return false
	}
}
function relinklinks() {
	var els = document.getElementsByClassName("threadlink")
	while (els.length) {
		els[0].onclick = pageswitcher("thread", els[0].text)
		els[0].classList.remove("threadlink")
	}
	els = document.getElementsByClassName("combolink")
	while (els.length) {
		els[0].onclick = pageswitcher("combo", els[0].text)
		els[0].classList.remove("combolink")
	}
	els = document.getElementsByClassName("authorlink")
	while (els.length) {
		var el = els[0]
		var xid = el.getAttribute("data-xid")
		el.onclick = pageswitcher("author", xid)
		el.classList.remove("authorlink")
	}
}
(function() {
	var el = document.getElementById("homelink")
	el.onclick = pageswitcher("home", "")
	el = document.getElementById("atmelink")
	el.onclick = pageswitcher("atme", "")
	el = document.getElementById("firstlink")
	el.onclick = pageswitcher("first", "")
	el = document.getElementById("savedlink")
	el.onclick = pageswitcher("saved", "")
	el = document.getElementById("longagolink")
	el.onclick = pageswitcher("longago", "")
	relinklinks()
	window.onpopstate = statechanger
	history.replaceState(curpagestate, "some title", "")
})();
(function() {
	hideelement("attachmentDescriptor")
})();
function showhonkform(elem, inReplyToID, hname) {
	var form = lehonkform
	form.style = "display: block"
	if (elem) {
		form.remove()
		elem.parentElement.parentElement.parentElement.insertAdjacentElement('beforebegin', form)
	} else {
		hideelement(lehonkbutton)
		elem = document.getElementById("honkformhost")
		elem.insertAdjacentElement('afterend', form)
	}
	var inReplyToIDinput = document.getElementById("inReplytoIDinput")
	if (inReplyToIDinput) {
		inReplytoIDinput.value = inReplytoID
		if (hname) {
			honkText.value = hname + " "
		} else {
			honkText.value = ""
		}
	} else {
		inReplyToIDinput.value = ""
		honkText.value = ""
	}
	var updateinput = document.getElementById("updatexidinput")
	updateinput.value = ""
	document.getElementById("honkText").focus()
	return false
}
function cancelhonking() {
	hideelement(lehonkform)
	showelement(lehonkbutton)
}
function showelement(el) {
	if (typeof(el) == "string")
		el = document.getElementById(el)
	if (!el) return
	el.style.display = "block"
}
function hideelement(el) {
	if (typeof(el) == "string")
		el = document.getElementById(el)
	if (!el) return
	el.style.display = "none"
}
function updateAttachment() {
	var el = document.getElementById("attachment")
	el.children[1].textContent = el.children[0].value.slice(-20)
	var el = document.getElementById("attachmentDescriptor")
	el.style.display = ""
	var el = document.getElementById("saveAttachmentXid")
	el.value = ""
}
var checkinprec = 100.0
var gpsoptions = {
	enableHighAccuracy: false,
	timeout: 1000,
	maximumAge: 0
};
function fillcheckin() {
	if (navigator.geolocation) {
		navigator.geolocation.getCurrentPosition(function(pos) {
			showelement("placedescriptor")
			var el = document.getElementById("placelatinput")
			el.value = Math.round(pos.coords.latitude * checkinprec) / checkinprec
			el = document.getElementById("placelonginput")
			el.value = Math.round(pos.coords.longitude * checkinprec) / checkinprec
			checkinprec = 10000.0
			gpsoptions.enableHighAccuracy = true
			gpsoptions.timeout = 2000
		}, function(err) {
			showelement("placedescriptor")
			el = document.getElementById("placenameinput")
			el.value = err.message
		}, gpsoptions)
	}
}
