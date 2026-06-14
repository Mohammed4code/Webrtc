// ─────────────────────────────────────────────────────────────────────────────
// WebRTC SIP Client - Frontend
// ─────────────────────────────────────────────────────────────────────────────

let ws = null;
let pc = null;
let localStream = null;
let currentDialedNumber = '';
let isRegistered = false;
let isInCall = false;
let myExtension = '';
let callTarget = '';

const $num = document.getElementById('dialedNumber');
const $state = document.getElementById('callState');
const $call = document.getElementById('callBtn');
const $hang = document.getElementById('hangupBtn');
const $status = document.getElementById('connectionStatus');

// ─── WebSocket ───────────────────────────────────────────────────────────────
function connectWS() {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    ws = new WebSocket(`${proto}//${location.host}/ws`);

    ws.onopen = () => {
        $status.textContent = '🟡 متصل بالخادم';
        $status.style.color = '#fbbf24';
    };

    ws.onmessage = async (ev) => {
        let m;
        try { m = JSON.parse(ev.data); } catch { return; }
        console.log('WS ←', m.type, m.status || '');

        switch (m.type) {
            case 'status': await handleStatus(m.status); break;
            case 'incoming': showIncoming(m.from, m.sdp); break;
            case 'answer': await applyRemoteAnswer(m.sdp); break;
        }
    };

    ws.onclose = () => {
        $status.textContent = '🔴 غير متصل';
        $status.style.color = '#ef4444';
        isRegistered = false;
        cleanupCall();
        setTimeout(connectWS, 3000);
    };
}

// ─── Status ──────────────────────────────────────────────────────────────────
async function handleStatus(s) {
    switch (s) {
        case 'registered':
            isRegistered = true;
            $status.textContent = '🟢 مسجل وجاهز';
            $status.style.color = '#4ade80';
            $state.textContent = '✅ جاهز';
            break;
        case 'auth_failed':
            isRegistered = false;
            $status.textContent = '🔴 كلمة مرور خاطئة';
            $status.style.color = '#ef4444';
            break;
        case 'connection_failed':
            $status.textContent = '🔴 تعذر الاتصال بـ Asterisk';
            $status.style.color = '#ef4444';
            break;
        case 'ringing':
            $state.textContent = '🔔 يرن...';
            break;
        case 'call_answered':
            isInCall = true;
            $state.textContent = '📞 متصل';
            $call.disabled = true;
            $hang.disabled = false;
            break;
        case 'call_failed':
            $state.textContent = '❌ فشل الاتصال';
            cleanupCall();
            break;
        case 'hangup':
            $state.textContent = '📴 انتهت المكالمة';
            cleanupCall();
            break;
    }
    updateDisplay();
}

// ─── تطبيق SDP Answer ──────────────────────────────────────────────────────
async function applyRemoteAnswer(sdp) {
    if (!pc) {
        console.error('❌ No RTCPeerConnection');
        return;
    }
    if (pc.signalingState !== 'have-local-offer') {
        console.warn('⚠️ PC state:', pc.signalingState);
        return;
    }
    try {
        await pc.setRemoteDescription({ type: 'answer', sdp });
        console.log('✅ Remote answer applied');
    } catch (e) {
        console.error('❌ setRemoteDescription failed:', e);
        $state.textContent = '❌ ' + e.message;
        cleanupCall();
    }
}

// ─── إنشاء PeerConnection ───────────────────────────────────────────────────
async function createPC() {
    localStream = await navigator.mediaDevices.getUserMedia({ audio: true, video: false });

    pc = new RTCPeerConnection({
        iceServers: [
            { urls: 'stun:stun.l.google.com:19302' },
            { urls: 'stun:stun1.l.google.com:19302' },
        ],
        bundlePolicy: 'max-bundle',
        rtcpMuxPolicy: 'require',
    });

    localStream.getTracks().forEach(t => pc.addTrack(t, localStream));

    pc.ontrack = (e) => {
        console.log('🎵 Remote audio track received!');
        const audio = document.getElementById('remoteAudio');
        if (e.streams && e.streams[0]) {
            audio.srcObject = e.streams[0];
        } else {
            const stream = new MediaStream([e.track]);
            audio.srcObject = stream;
        }
        audio.play().catch(err => {
            console.warn('⚠️ Autoplay blocked:', err);
            document.addEventListener('click', () => audio.play(), { once: true });
        });
    };

    pc.onconnectionstatechange = () => {
        console.log('🔗 PC state:', pc.connectionState);
        if (['disconnected', 'failed', 'closed'].includes(pc.connectionState)) {
            if (isInCall) cleanupCall();
        }
    };

    return pc;
}

// ─── Outgoing Call ───────────────────────────────────────────────────────────
async function makeCall() {
    if (!currentDialedNumber || !isRegistered || isInCall) return;
    callTarget = currentDialedNumber;

    try {
        $state.textContent = '🎤 تجهيز الميكروفون...';
        await createPC();

        $state.textContent = '❄️ جاري تجهيز الاتصال...';
        const offer = await pc.createOffer({
            offerToReceiveAudio: true,
            offerToReceiveVideo: false,
        });

        offer.sdp = fixSDPForAsterisk(offer.sdp);
        await pc.setLocalDescription(offer);
        await waitForICEComplete(pc);

        const finalSDP = pc.localDescription.sdp;
        
        if (!finalSDP.includes('a=candidate')) {
            console.error('❌ No ICE candidates!');
            $state.textContent = '❌ فشل جمع ICE';
            cleanupCall();
            return;
        }

        ws.send(JSON.stringify({
            type: 'call',
            extension: myExtension,
            target: callTarget,
            sdp: finalSDP,
        }));

        $state.textContent = '⏳ جاري الاتصال...';
        $call.disabled = true;
        console.log('📞 SDP Offer sent');

    } catch (e) {
        console.error('makeCall error:', e);
        $state.textContent = '❌ ' + e.message;
        cleanupCall();
    }
}

// ─── Incoming Call ───────────────────────────────────────────────────────────
function showIncoming(from, sdp) {
    document.getElementById('incomingNumber').textContent = from || 'غير معروف';
    document.getElementById('incomingPanel').classList.remove('hidden');
    $state.textContent = '📞 مكالمة واردة من ' + (from || '؟');
    window._incomingSDP = sdp;
}

async function answerCall() {
    document.getElementById('incomingPanel').classList.add('hidden');
    const offerSDP = window._incomingSDP;
    if (!offerSDP) return;

    try {
        $state.textContent = '🎤 تجهيز الميكروفون...';
        await createPC();

        await pc.setRemoteDescription({ type: 'offer', sdp: offerSDP });
        const answer = await pc.createAnswer();
        answer.sdp = fixSDPForAsterisk(answer.sdp);
        await pc.setLocalDescription(answer);
        await waitForICEComplete(pc);

        const finalSDP = pc.localDescription.sdp;

        ws.send(JSON.stringify({
            type: 'answer',
            extension: myExtension,
            target: 'caller',
            sdp: finalSDP,
        }));

        isInCall = true;
        $state.textContent = '📞 متصل';
        $hang.disabled = false;
        $call.disabled = true;
        updateDisplay();

    } catch (e) {
        console.error('answerCall error:', e);
        $state.textContent = '❌ ' + e.message;
        cleanupCall();
    }
}

function rejectCall() {
    document.getElementById('incomingPanel').classList.add('hidden');
    ws?.send(JSON.stringify({ type: 'reject', extension: myExtension }));
    $state.textContent = '';
    updateDisplay();
}

// ─── Hangup ──────────────────────────────────────────────────────────────────
function hangup() {
    ws?.send(JSON.stringify({ type: 'hangup', extension: myExtension, target: callTarget }));
    cleanupCall();
}

function cleanupCall() {
    isInCall = false;
    callTarget = '';
    if (pc) { pc.close(); pc = null; }
    if (localStream) { localStream.getTracks().forEach(t => t.stop()); localStream = null; }
    const audio = document.getElementById('remoteAudio');
    audio.srcObject = null; audio.pause();
    document.getElementById('incomingPanel').classList.add('hidden');
    updateDisplay();
}

// ─── Register ─────────────────────────────────────────────────────────────────
function register() {
    const ext = document.getElementById('extension').value.trim();
    const pass = document.getElementById('password').value.trim();
    const srv = document.getElementById('server').value.trim();
    if (!ext || !pass || !srv) { alert('أكمل البيانات'); return; }
    myExtension = ext;
    
    if (!ws || ws.readyState !== WebSocket.OPEN) {
        connectWS();
        const checkInterval = setInterval(() => {
            if (ws && ws.readyState === WebSocket.OPEN) {
                clearInterval(checkInterval);
                ws.send(JSON.stringify({ type: 'register', extension: ext, password: pass, target: srv }));
            }
        }, 100);
    } else {
        ws.send(JSON.stringify({ type: 'register', extension: ext, password: pass, target: srv }));
    }
    $status.textContent = '🟡 جاري التسجيل...';
}

// ─── SDP Fix ─────────────────────────────────────────────────────────────────
function fixSDPForAsterisk(sdp) {
    sdp = sdp.replace(/a=recvonly/g, 'a=sendrecv');
    sdp = sdp.replace(/a=sendonly/g, 'a=sendrecv');
    sdp = sdp.replace(/a=inactive/g, 'a=sendrecv');
    return sdp;
}

// ─── ICE Gathering ───────────────────────────────────────────────────────────
function waitForICEComplete(peerConn) {
    return new Promise(resolve => {
        if (peerConn.iceGatheringState === 'complete') { resolve(); return; }
        const timeout = setTimeout(() => {
            console.warn('⚠️ ICE gathering timeout');
            resolve();
        }, 8000);
        peerConn.addEventListener('icegatheringstatechange', function handler() {
            if (peerConn.iceGatheringState === 'complete') {
                clearTimeout(timeout);
                peerConn.removeEventListener('icegatheringstatechange', handler);
                resolve();
            }
        });
    });
}

// ─── Dialpad ─────────────────────────────────────────────────────────────────
function addDigit(d) {
    if (isInCall) {
        ws?.send(JSON.stringify({ type: 'dtmf', extension: myExtension, digit: d }));
    } else {
        currentDialedNumber += d;
        updateDisplay();
    }
}

function clearNumber() {
    if (!isInCall) { currentDialedNumber = ''; updateDisplay(); }
}

function updateDisplay() {
    $num.textContent = currentDialedNumber || '🔢 أدخل الرقم';
    $call.disabled = !isRegistered || isInCall || !currentDialedNumber;
    $hang.disabled = !isInCall;
}

// ─── Bindings ─────────────────────────────────────────────────────────────────
document.querySelectorAll('.dial-btn').forEach(btn =>
    btn.addEventListener('click', () => addDigit(btn.dataset.digit))
);

window.register = register;
window.makeCall = makeCall;
window.hangup = hangup;
window.clearNumber = clearNumber;
window.answerCall = answerCall;
window.rejectCall = rejectCall;

updateDisplay();
connectWS();