function getBaseUrl() {
    var ishttps = 'https:' == document.location.protocol ? true : false;
    var url = window.location.host;
    if (ishttps) {
        url = 'https://' + url;
    } else {
        url = 'http://' + url;
    }
    return url;
}
function getWsBaseUrl() {
    var ishttps = 'https:' == document.location.protocol ? true : false;
    var url = window.location.host;
    if (ishttps) {
        url = 'wss://' + url;
    } else {
        url = 'ws://' + url;
    }
    return url;
}
//除去html标签
function replaceHtml(str){
    return str.replace(/<[^>]*>/g, '');
}
//浏览器桌面通知
function notify(title, options, callback) {

    // 先检查浏览器是否支持
    if (!window.Notification) {
        console.log("浏览器不支持notify");
        return;
    }
    options.body=replaceHtml(options.body);
    console.log("浏览器notify权限:", Notification.permission);
    // 检查用户曾经是否同意接受通知
    if (Notification.permission === 'granted') {
        var notification = new Notification(title, options); // 显示通知
        if (notification && callback) {
            notification.onclick = function(event) {
                callback(notification, event);
            }
            setTimeout(function () {
                notification.close();
            },3000);
        }
    } else {
        Notification.requestPermission().then( (permission) =>function(){
            console.log("请求浏览器notify权限:", permission);
            if (permission === 'granted') {
                notification = new Notification(title, options); // 显示通知
                if (notification && callback) {
                    notification.onclick = function (event) {
                        callback(notification, event);
                    }
                    setTimeout(function () {
                        notification.close();
                    }, 3000);
                }
            } else if (permission === 'default') {
                console.log('用户关闭授权 可以再次请求授权');
            } else {
                console.log('用户拒绝授权 不能显示通知');
            }
        });
    }

}
var titleTimer=0;
var titleNum=0;
var originTitle = document.title;
function flashTitle() {
    if(titleTimer!=0){
        return;
    }
    titleTimer = setInterval(function(){
        titleNum++;
        if (titleNum == 3) {
            titleNum = 1;
        }
        if (titleNum == 1) {
            document.title = '【】' + originTitle;
        }
        if (titleNum == 2) {
            document.title = '【new message】' + originTitle;
        }
    }, 500);

}
function clearFlashTitle() {
    clearInterval(titleTimer);
    document.title = originTitle;
}
var faceTitles = ["[微笑]", "[嘻嘻]", "[哈哈]", "[可爱]", "[可怜]", "[挖鼻]", "[吃惊]", "[害羞]", "[挤眼]", "[闭嘴]", "[鄙视]", "[爱你]", "[泪]", "[偷笑]", "[亲亲]", "[生病]", "[太开心]", "[白眼]", "[右哼哼]", "[左哼哼]", "[嘘]", "[衰]", "[委屈]", "[吐]", "[哈欠]", "[抱抱]", "[怒]", "[疑问]", "[馋嘴]", "[拜拜]", "[思考]", "[汗]", "[困]", "[睡]", "[钱]", "[失望]", "[酷]", "[色]", "[哼]", "[鼓掌]", "[晕]", "[悲伤]", "[抓狂]", "[黑线]", "[阴险]", "[怒骂]", "[互粉]", "[心]", "[伤心]", "[猪头]", "[熊猫]", "[兔子]", "[ok]", "[耶]", "[good]", "[NO]", "[赞]", "[来]", "[弱]", "[草泥马]", "[神马]", "[囧]", "[浮云]", "[给力]", "[围观]", "[威武]", "[奥特曼]", "[礼物]", "[钟]", "[话筒]", "[蜡烛]", "[蛋糕]"];
function placeFace() {
    var faces=[];
    for(var i=0;i<faceTitles.length;i++){
        faces[faceTitles[i]]="/static/images/face/"+i+".gif";
    }
    return faces;
}
function escapeHtml(value) {
    return String(value || '').replace(/[&<>'"]/g, function (char) {
        return {'&':'&amp;','<':'&lt;','>':'&gt;',"'":'&#39;','"':'&quot;'}[char];
    });
}
function safeUrl(value) {
    try {
        var url = new URL(String(value || ''), window.location.origin);
        if (url.protocol !== 'http:' && url.protocol !== 'https:') return '';
        return escapeHtml(url.href);
    } catch (_) {
        return '';
    }
}
function renderMessageToken(token, baseUrl) {
    var faces = placeFace();
    if (token.indexOf('face[') === 0) {
        var face = token.replace(/^face/g, '');
        var facePath = faces[face];
        return facePath ? '<img alt="' + escapeHtml(face) + '" title="' + escapeHtml(face) + '" src="' + escapeHtml(baseUrl + facePath) + '">' : escapeHtml(token);
    }
    if (token.indexOf('img[') === 0) {
        var src = token.replace(/^img\[/, '').replace(/\]$/, '');
        var safeSrc = safeUrl(src);
        return safeSrc ? '<img onclick="bigPic(this.src,true)" src="' + safeSrc + '" style="max-width: 150px"/>' : escapeHtml(token);
    }
    if (token.indexOf('attachment[') === 0) {
        try {
            var info = JSON.parse(token.slice(11, -1));
            var path = safeUrl(info.path);
            if (!path) return escapeHtml(token);
            var imgSrc = '/static/images/ext/default.png';
            var icons = {'.mp3':'MP3','.zip':'ZIP','.txt':'TXT','.7z':'7z','.bpm':'BMP','.png':'PNG','.jpg':'JPG','.jpeg':'JPEG','.pdf':'PDF','.doc':'DOC','.docx':'DOCX','.rar':'RAR','.xlsx':'XLSX','.csv':'XLSX'};
            if (icons[info.ext]) imgSrc = '/static/images/ext/' + icons[info.ext] + '.png';
            return '<div data-url="' + path + '" onclick="window.open(this.dataset.url)" class="productCard">' +
                '<div><img src="' + imgSrc + '" style="width: 38px;height: 38px;" /></div>' +
                '<div class="productCardTitle"><div class="productCardTitle">' + escapeHtml(info.name) + '</div>' +
                '<div style="font-size: 12px;color: #666">' + formatFileSize(Number(info.size) || 0) + '</div></div></div>';
        } catch (_) {
            return escapeHtml(token);
        }
    }
    return escapeHtml(token);
}
function replaceContent(content, baseUrl) {
    baseUrl = baseUrl || '';
    var tokens = [];
    var text = String(content || '').replace(/face\[[^\]]*\]|img\[[^\]]*\]|attachment\[[\s\S]*?\]/g, function (token) {
        var index = tokens.push(token) - 1;
        return '\u0000' + index + '\u0000';
    });
    text = escapeHtml(text).replace(/\n/g, '<br>');
    return text.replace(/\u0000(\d+)\u0000/g, function (_, index) {
        return renderMessageToken(tokens[Number(index)], baseUrl);
    });
}

function formatFileSize(fileSize) {
    if (fileSize < 1024) {
        return fileSize + 'B';
    } else if (fileSize < (1024*1024)) {
        var temp = fileSize / 1024;
        temp = temp.toFixed(2);
        return temp + 'KB';
    } else if (fileSize < (1024*1024*1024)) {
        var temp = fileSize / (1024*1024);
        temp = temp.toFixed(2);
        return temp + 'MB';
    } else {
        var temp = fileSize / (1024*1024*1024);
        temp = temp.toFixed(2);
        return temp + 'GB';
    }
}
function bigPic(src,isVisitor){
    if (isVisitor) {
        window.open(src);
        return;
    }
}
function filter (obj){
    var imgType = ["image/jpeg","image/png","image/jpg","image/gif"];
    var filetypes = imgType;
    var isnext = false;
    for (var i = 0; i < filetypes.length; i++) {
        if (filetypes[i] == obj.type) {
            return true;
        }
    }
    return false;
}
function sleep(time) {
    var startTime = new Date().getTime() + parseInt(time, 10);
    while(new Date().getTime() < startTime) {}
}
function checkLang(){
    var langs=["cn","en"];
    var lang=getQuery("lang");
    if(lang!=""&&langs.indexOf(lang) > 0 ){
        return lang;
    }
    return "cn";
}
function getQuery(key) {
    return new URLSearchParams(window.location.search).get(key) || "";
}
function utf8ToB64(str) {
    return window.btoa(unescape(encodeURIComponent(str)));
}
function b64ToUtf8(str) {
    return decodeURIComponent(escape(window.atob(str)));
}
//播放声音
function alertSound(id,src){
    var b = document.getElementById(id);
    if(src!=""){
        b.src=src;
    }
    var p = b.play();
    p && p.then(function(){}).catch(function(e){});
}
//日期格式化
function formatDate(dateString, format = 'yyyy-MM-dd HH:mm:ss') {
    const date = new Date(dateString);

    const year = date.getFullYear();
    const month = String(date.getMonth() + 1).padStart(2, '0');
    const day = String(date.getDate()).padStart(2, '0');
    const hour = String(date.getHours()).padStart(2, '0');
    const minute = String(date.getMinutes()).padStart(2, '0');
    const second = String(date.getSeconds()).padStart(2, '0');

    const formattedDate = format
        .replace(/yyyy/g, year)
        .replace(/MM/g, month)
        .replace(/dd/g, day)
        .replace(/HH/g, hour)
        .replace(/mm/g, minute)
        .replace(/ss/g, second);

    return formattedDate;
}
function copyText(text) {
    var target = document.createElement('input')
    target.value = text
    document.body.appendChild(target)
    target.select()
    document.execCommand("copy");
    document.body.removeChild(target);
    return true;
}
;
