const assert = require('node:assert/strict');
const { test } = require('node:test');
const { retain, visibleRange } = require('../static/js/chat-window.js');
const rows = (from, count) => Array.from({length:count}, (_,i)=>({id:from+i}));

test('scroll events never fetch older history, including a latest page shorter than the viewport', () => {
    const {component}=require('../static/js/chat-window.js');
    for(const scrollHeight of [240,1000]){
        const requests=[];
        const state={_dead:false,_restoring:false,_edge:'',older:true,newer:false,loading:false,
            items:rows(100,5),key:item=>item.id,
            $el:{scrollTop:0,clientHeight:600,scrollHeight},
            remember(){},updateRange(){},publish(){},request:direction=>requests.push(direction)};
        component.methods.onScroll.call(state);
        state.items.push({id:105});
        component.methods.onScroll.call(state);
        assert.deepEqual(requests,[]);
    }
});

test('explicit older request preserves the reading anchor and respects the loading lock', () => {
    const {component}=require('../static/js/chat-window.js');
    const requests=[];let anchors=0;
    const state={loading:false,_bottom:true,remember(){anchors++;},$emit:event=>requests.push(event)};
    component.methods.request.call(state,'older');
    assert.deepEqual(requests,['older']);assert.equal(anchors,1);assert.equal(state._bottom,false);
    state.loading=true;component.methods.request.call(state,'older');
    assert.deepEqual(requests,['older']);
});

test('scrolling to the newer edge can still fill a retained history window', () => {
    const {component}=require('../static/js/chat-window.js');
    const requests=[];
    const state={_dead:false,_restoring:false,_edge:'',older:true,newer:true,loading:false,
        items:rows(100,5),key:item=>item.id,$el:{scrollTop:400,clientHeight:600,scrollHeight:1000},
        remember(){},updateRange(){},publish(){},request:event=>requests.push(event)};
    component.methods.onScroll.call(state);component.methods.onScroll.call(state);
    assert.deepEqual(requests,['newer']);
});
test('older pages evict the newest data while keeping the pending send', () => {
    const pending={sending:true,content:'draft'};
    const result=retain(rows(1,220).concat(pending),'older');
    assert.equal(result.items.length,200);
    assert.equal(result.items[0].id,1);
    assert.equal(result.items[198].id,199);
    assert.equal(result.items[199],pending);
    assert.equal(result.newer,true);
});
test('newer pages evict old data and remain bounded across long histories', () => {
    let items=[];
    for(let i=0;i<500;i++) items=retain(items.concat(rows(i*20+1,20)),'newer').items;
    assert.equal(items.length,200);
    assert.equal(items[0].id,9801);
    assert.equal(items.at(-1).id,10000);
});
test('virtual range only renders the viewport and a small buffer', () => {
    const heights=Array(10000).fill(80);
    const r=visibleRange(heights,400000,600);
    assert.ok(r.start<=5000 && r.end>5007);
    assert.ok(r.end-r.start<=20);
    assert.equal(r.top,r.start*80);
    assert.equal(r.bottom,(10000-r.end)*80);
});
test('variable image heights and an empty list are handled', () => {
    const r=visibleRange([30,900,40,300,20],930,100,0);
    assert.equal(r.start,2);
    assert.equal(r.end,4);
    assert.deepEqual(visibleRange([],0,500),{start:0,end:0,top:0,bottom:0});
});

test('a gesture without scrolling does not disable following the bottom or discard the reading anchor', () => {
    const {component}=require('../static/js/chat-window.js');
    const cancel=global.cancelAnimationFrame;global.cancelAnimationFrame=()=>{};
    try {
        for(const event of [{type:'wheel'},{type:'touchstart'},{type:'keydown',key:'End'}]){
            const anchor={key:'m200',offset:0};
            const state={_settle:0,_restoring:true,_bottom:true,_anchor:anchor};
            component.methods.interact.call(state,event);
            assert.equal(state._bottom,true);
            assert.equal(state._anchor,anchor);
        }
    } finally {global.cancelAnimationFrame=cancel;}
});

test('measuring unchanged rows does not write scrollTop and interrupt native momentum', () => {
    const {component}=require('../static/js/chat-window.js');
    let writes=0;
    const el={clientHeight:300,scrollHeight:800,clientTop:0,get scrollTop(){return 500;},set scrollTop(value){writes++;}};
    const state={_dead:false,$el:el,_sizes:new Map(),_settle:0,_bottom:true,
        rows:()=>[],heights:()=>[],updateRange:()=>false,remember(){},publish(){}};
    const raf=global.requestAnimationFrame,caf=global.cancelAnimationFrame;
    global.requestAnimationFrame=()=>1;global.cancelAnimationFrame=()=>{};
    try {component.methods.measure.call(state);assert.equal(writes,0);}
    finally {global.requestAnimationFrame=raf;global.cancelAnimationFrame=caf;}
});
