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
test('history pagination and latest refresh retain failed sends alongside pending sends', () => {
    const failed={failed:true,content:'retry me',client_message_id:'original-key'};
    const pending={sending:true,content:'sending'};
    for(const direction of ['older','newer']){
        const result=retain(rows(1,220).concat(failed,pending),direction);
        assert.equal(result.items.length,200);
        assert.deepEqual(result.items.slice(-2),[failed,pending]);
        assert.equal(failed.client_message_id,'original-key');
        assert.equal(result.items[0].id,direction==='older'?1:23);
        assert.equal(result.items[197].id,direction==='older'?198:220);
        assert.equal(result.newer,direction==='older');
        assert.equal(result.older,direction==='newer');
    }
});
test('local sends do not mark persisted history as overflowing and remain bounded', () => {
    const local=Array.from({length:201},(_,i)=>({failed:true,content:String(i)}));
    const result=retain(local,'older');
    assert.equal(result.items.length,200);
    assert.deepEqual(result.items,local.slice(-200));
    assert.equal(result.older,false);
    assert.equal(result.newer,false);
    const exact=retain(rows(1,198).concat(local.slice(0,2)),'older');
    assert.equal(exact.newer,false);
});
test('history controls use clear labels and show loading state', () => {
    const {component}=require('../static/js/chat-window.js');
    const h=(tag,data,children)=>({tag,data,children});
    for(const loading of [false,true]){
        const state={older:true,newer:true,loading,items:[],range:{top:0,bottom:0,start:0,end:0},request(){}};
        const tree=component.render.call(state,h);
        const buttons=tree.children.filter(node=>node.tag==='button');
        assert.equal(buttons[0].children[0],loading?'正在加载…':'查看更早消息');
        assert.equal(buttons.at(-1).children[0],'有新消息，回到底部');
        assert.ok(buttons.every(button=>button.data.attrs.disabled===loading));
    }
});

test('44px history control is included in virtual ranges and preserved reading anchors', () => {
    const {component}=require('../static/js/chat-window.js');
    const state=Object.assign({},component.methods,{
        older:true,items:rows(1,30),_sizes:new Map(),_bottom:false,_dead:false,_settle:0,
        range:{},rows:()=>[],key:item=>String(item.id),heights(){return this.items.map(()=>80);},
        $el:{scrollTop:844,clientTop:0,clientHeight:150,scrollHeight:3000,
            getBoundingClientRect:()=>({top:0}),querySelector:()=>({offsetHeight:44})},
        publish(){},
    });
    state.remember();
    assert.deepEqual(state._anchor,{key:'11',offset:0});
    state.updateRange();
    assert.deepEqual(state.range,visibleRange(state.heights(),800,150));
    state.items=rows(-1,2).concat(state.items);
    const raf=global.requestAnimationFrame,caf=global.cancelAnimationFrame;
    global.requestAnimationFrame=()=>1;global.cancelAnimationFrame=()=>{};
    try {
        state.measure();
        assert.equal(state.$el.scrollTop,1004);
    } finally {global.requestAnimationFrame=raf;global.cancelAnimationFrame=caf;}
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
