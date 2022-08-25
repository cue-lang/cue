// Copyright 2020 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package adt_test

import (
	"flag"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/tools/txtar"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/debug"
	"cuelang.org/go/internal/core/eval"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/core/validate"
	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/internal/cuetxtar"
	_ "cuelang.org/go/pkg"
)

var (
	todo = flag.Bool("todo", false, "run tests marked with #todo-compile")
)

func TestEval(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root:   "../../../cue/testdata",
		Name:   "eval",
		Update: cuetest.UpdateGoldenFiles,
		Skip:   alwaysSkip,
		ToDo:   needFix,
	}

	if *todo {
		test.ToDo = nil
	}

	r := runtime.New()

	test.Run(t, func(t *cuetxtar.Test) {
		a := t.ValidInstances()

		v, err := r.Build(nil, a[0])
		if err != nil {
			t.WriteErrors(err)
			return
		}

		e := eval.New(r)
		ctx := e.NewContext(v)
		v.Finalize(ctx)

		stats := ctx.Stats()
		t.Log(stats)
		// if n := stats.Leaks(); n > 0 {
		// 	t.Skipf("%d leaks reported", n)
		// }

		if b := validate.Validate(ctx, v, &validate.Config{
			AllErrors: true,
		}); b != nil {
			fmt.Fprintln(t, "Errors:")
			t.WriteErrors(b.Err)
			fmt.Fprintln(t, "")
			fmt.Fprintln(t, "Result:")
		}

		if v == nil {
			return
		}

		debug.WriteNode(t, r, v, &debug.Config{Cwd: t.Dir})
		fmt.Fprintln(t)
	})
}

var alwaysSkip = map[string]string{
	"compile/erralias": "compile error",
}

var needFix = map[string]string{
	"DIR/NAME": "reason",
}

// TestX is for debugging. Do not delete.
func TestX(t *testing.T) {
	in := `
-- cue.mod/module.cue --
module: "mod.test"

-- in.cue --
package foo

// #d: l: _
// e: #D  & {
// 	l: g: int
// }
// #D: {l: _}



// noChildError: issue1882: v1: {
// 	#Output: output?: #Output
// 	#Output: #Output
// 	o: #Output
// }

// #A: a: b: {
// 	c: string
// 	d: 2
// }
// #B: a: b: {
// 	c: *"d" | string
// 	(c): 2
// }
// x: #A & #B
// v: x.a & {
// 	b: c: "e"
// }

// #complex: things: [string]: 2

// many: #complex
// many: things: hola: 2

// // Now inner struct on second struct is NOT considered closed
// notbad: many
// notbad: things: hola: 2


// t10: {
// 	schema: test
// 	#A: {string | {name: string}}

// 	test: name: "Test"
// 	test: #A
// }

// #Output: {stdout: null} |
//  {url: string} |
//  {retry: {output: #Output }}

// o: #Output & {url: "test"}

// #Output2: {
// 	numexist(<=1, url, stdout, retry)
// 	url?:    string
// 	stdout?: null
// 	retry?:  output: #Output
// }

// t1: {
// 	A: x:  string | A
// 	C: A
// }

// t1: {
// 	a: ["3"] + a
// 	a: ["1", "2"]
// }

// crossRefNoCycle: t4: {
// 	T: {
// 		x: _
// 		y: x
// 	}
// 	C: T & { x: T & { x: T } }
// }


// issue783: {
// 	test1: {
// 		string
// 		#foo: "bar"
// 	}

// 	test2: {
// 		hello: "world"
// 		#foo:  "bar"
// 	}


// #theme: x: {
// 	color:   string
// 	ctermbg: string
// }
// y: #theme.x
// dark: y & {
// 	color:   "dark"
// 	ctermbg: "239"
// }

// reg: {foo: 1}
// #def: sub: reg
// val: #def

// A: close({
// 	a: 1
// 	b: 2
// })

// B: A & {
// 	c: 3
// }




// 	#Def1: {
// 		...
// 		#foo: string
// 	} | {
// 		string
// 		#foo: string
// 	}
// 	check1a: test1 & #Def1
// 	check1b: test2 & #Def1

// 	#Def2: {
// 		...
// 		#foo: string
// 		_
// 	}
// 	check2a: test1 & #Def2
// 	check2b: test2 & #Def2
// }

// f1: string
// f2: f1
// f3: f2
// f4:    f3
// f5:    f4
// f6:    f5
// f7:    f6
// f8:    f7
// f9:    f8
// f10:   f9
// f11:   f10
// f12:   f11
// f13:   f12
// f14:   f13
// f15:   f14
// f16:   f15
// f17:   f16
// f18:   f17
// f19:   f18
// f20:   f19
// f21:   f20
// f22:   f21
// f23:   f22
// f24:   f23
// f25:   f24
// f26:   f25
// f27:   f26
// f28:   f27
// f29:   f28
// f30:   f29
// f31:   f30
// f32:   f31
// f33:   f32
// f34:   f33
// f35:   f34
// f36:   f35
// f37:   f36
// f38:   f37
// f39:   f38
// f40:   f39
// f41:   f40
// f42:   f41
// f43:   f42
// f44:   f43
// f45:   f44
// f46:   f45
// f47:   f46
// f48:   f47
// f49:   f48
// f50:   f49
// f51:   f50
// f52:   f51
// f53:   f52
// f54:   f53
// f55:   f54
// f56:   f55
// f57:   f56
// f58:   f57
// f59:   f58
// f60:   f59
// f61:   f60
// f62:   f61
// f63:   f62
// f64:   f63
// f65:   f64
// f66:   f65
// f67:   f66
// f68:   f67
// f69:   f68
// f70:   f69
// f71:   f70
// f72:   f71
// f73:   f72
// f74:   f73
// f75:   f74
// f76:   f75
// f77:   f76
// f78:   f77
// f79:   f78
// f80:   f79
// f81:   f80
// f82:   f81
// f83:   f82
// f84:   f83
// f85:   f84
// f86:   f85
// f87:   f86
// f88:   f87
// f89:   f88
// f90:   f89
// f91:   f90
// f92:   f91
// f93:   f92
// f94:   f93
// f95:   f94
// f96:   f95
// f97:   f96
// f98:   f97
// f99:   f98
// f100:  f99
// f101:  f100
// f102:  f101
// f103:  f102
// f104:  f103
// f105:  f104
// f106:  f105
// f107:  f106
// f108:  f107
// f109:  f108
// f110:  f109
// f111:  f110
// f112:  f111
// f113:  f112
// f114:  f113
// f115:  f114
// f116:  f115
// f117:  f116
// f118:  f117
// f119:  f118
// f120:  f119
// f121:  f120
// f122:  f121
// f123:  f122
// f124:  f123
// f125:  f124
// f126:  f125
// f127:  f126
// f128:  f127
// f129:  f128
// f130:  f129
// f131:  f130
// f132:  f131
// f133:  f132
// f134:  f133
// f135:  f134
// f136:  f135
// f137:  f136
// f138:  f137
// f139:  f138
// f140:  f139
// f141:  f140
// f142:  f141
// f143:  f142
// f144:  f143
// f145:  f144
// f146:  f145
// f147:  f146
// f148:  f147
// f149:  f148
// f150:  f149
// f151:  f150
// f152:  f151
// f153:  f152
// f154:  f153
// f155:  f154
// f156:  f155
// f157:  f156
// f158:  f157
// f159:  f158
// f160:  f159
// f161:  f160
// f162:  f161
// f163:  f162
// f164:  f163
// f165:  f164
// f166:  f165
// f167:  f166
// f168:  f167
// f169:  f168
// f170:  f169
// f171:  f170
// f172:  f171
// f173:  f172
// f174:  f173
// f175:  f174
// f176:  f175
// f177:  f176
// f178:  f177
// f179:  f178
// f180:  f179
// f181:  f180
// f182:  f181
// f183:  f182
// f184:  f183
// f185:  f184
// f186:  f185
// f187:  f186
// f188:  f187
// f189:  f188
// f190:  f189
// f191:  f190
// f192:  f191
// f193:  f192
// f194:  f193
// f195:  f194
// f196:  f195
// f197:  f196
// f198:  f197
// f199:  f198
// f200:  f199
// f201:  f200
// f202:  f201
// f203:  f202
// f204:  f203
// f205:  f204
// f206:  f205
// f207:  f206
// f208:  f207
// f209:  f208
// f210:  f209
// f211:  f210
// f212:  f211
// f213:  f212
// f214:  f213
// f215:  f214
// f216:  f215
// f217:  f216
// f218:  f217
// f219:  f218
// f220:  f219
// f221:  f220
// f222:  f221
// f223:  f222
// f224:  f223
// f225:  f224
// f226:  f225
// f227:  f226
// f228:  f227
// f229:  f228
// f230:  f229
// f231:  f230
// f232:  f231
// f233:  f232
// f234:  f233
// f235:  f234
// f236:  f235
// f237:  f236
// f238:  f237
// f239:  f238
// f240:  f239
// f241:  f240
// f242:  f241
// f243:  f242
// f244:  f243
// f245:  f244
// f246:  f245
// f247:  f246
// f248:  f247
// f249:  f248
// f250:  f249
// f251:  f250
// f252:  f251
// f253:  f252
// f254:  f253
// f255:  f254
// f256:  f255
// f257:  f256
// f258:  f257
// f259:  f258
// f260:  f259
// f261:  f260
// f262:  f261
// f263:  f262
// f264:  f263
// f265:  f264
// f266:  f265
// f267:  f266
// f268:  f267
// f269:  f268
// f270:  f269
// f271:  f270
// f272:  f271
// f273:  f272
// f274:  f273
// f275:  f274
// f276:  f275
// f277:  f276
// f278:  f277
// f279:  f278
// f280:  f279
// f281:  f280
// f282:  f281
// f283:  f282
// f284:  f283
// f285:  f284
// f286:  f285
// f287:  f286
// f288:  f287
// f289:  f288
// f290:  f289
// f291:  f290
// f292:  f291
// f293:  f292
// f294:  f293
// f295:  f294
// f296:  f295
// f297:  f296
// f298:  f297
// f299:  f298
// f300:  f299
// f301:  f300
// f302:  f301
// f303:  f302
// f304:  f303
// f305:  f304
// f306:  f305
// f307:  f306
// f308:  f307
// f309:  f308
// f310:  f309
// f311:  f310
// f312:  f311
// f313:  f312
// f314:  f313
// f315:  f314
// f316:  f315
// f317:  f316
// f318:  f317
// f319:  f318
// f320:  f319
// f321:  f320
// f322:  f321
// f323:  f322
// f324:  f323
// f325:  f324
// f326:  f325
// f327:  f326
// f328:  f327
// f329:  f328
// f330:  f329
// f331:  f330
// f332:  f331
// f333:  f332
// f334:  f333
// f335:  f334
// f336:  f335
// f337:  f336
// f338:  f337
// f339:  f338
// f340:  f339
// f341:  f340
// f342:  f341
// f343:  f342
// f344:  f343
// f345:  f344
// f346:  f345
// f347:  f346
// f348:  f347
// f349:  f348
// f350:  f349
// f351:  f350
// f352:  f351
// f353:  f352
// f354:  f353
// f355:  f354
// f356:  f355
// f357:  f356
// f358:  f357
// f359:  f358
// f360:  f359
// f361:  f360
// f362:  f361
// f363:  f362
// f364:  f363
// f365:  f364
// f366:  f365
// f367:  f366
// f368:  f367
// f369:  f368
// f370:  f369
// f371:  f370
// f372:  f371
// f373:  f372
// f374:  f373
// f375:  f374
// f376:  f375
// f377:  f376
// f378:  f377
// f379:  f378
// f380:  f379
// f381:  f380
// f382:  f381
// f383:  f382
// f384:  f383
// f385:  f384
// f386:  f385
// f387:  f386
// f388:  f387
// f389:  f388
// f390:  f389
// f391:  f390
// f392:  f391
// f393:  f392
// f394:  f393
// f395:  f394
// f396:  f395
// f397:  f396
// f398:  f397
// f399:  f398
// f400:  f399
// f401:  f400
// f402:  f401
// f403:  f402
// f404:  f403
// f405:  f404
// f406:  f405
// f407:  f406
// f408:  f407
// f409:  f408
// f410:  f409
// f411:  f410
// f412:  f411
// f413:  f412
// f414:  f413
// f415:  f414
// f416:  f415
// f417:  f416
// f418:  f417
// f419:  f418
// f420:  f419
// f421:  f420
// f422:  f421
// f423:  f422
// f424:  f423
// f425:  f424
// f426:  f425
// f427:  f426
// f428:  f427
// f429:  f428
// f430:  f429
// f431:  f430
// f432:  f431
// f433:  f432
// f434:  f433
// f435:  f434
// f436:  f435
// f437:  f436
// f438:  f437
// f439:  f438
// f440:  f439
// f441:  f440
// f442:  f441
// f443:  f442
// f444:  f443
// f445:  f444
// f446:  f445
// f447:  f446
// f448:  f447
// f449:  f448
// f450:  f449
// f451:  f450
// f452:  f451
// f453:  f452
// f454:  f453
// f455:  f454
// f456:  f455
// f457:  f456
// f458:  f457
// f459:  f458
// f460:  f459
// f461:  f460
// f462:  f461
// f463:  f462
// f464:  f463
// f465:  f464
// f466:  f465
// f467:  f466
// f468:  f467
// f469:  f468
// f470:  f469
// f471:  f470
// f472:  f471
// f473:  f472
// f474:  f473
// f475:  f474
// f476:  f475
// f477:  f476
// f478:  f477
// f479:  f478
// f480:  f479
// f481:  f480
// f482:  f481
// f483:  f482
// f484:  f483
// f485:  f484
// f486:  f485
// f487:  f486
// f488:  f487
// f489:  f488
// f490:  f489
// f491:  f490
// f492:  f491
// f493:  f492
// f494:  f493
// f495:  f494
// f496:  f495
// f497:  f496
// f498:  f497
// f499:  f498
// f500:  f499
// f501:  f500
// f502:  f501
// f503:  f502
// f504:  f503
// f505:  f504
// f506:  f505
// f507:  f506
// f508:  f507
// f509:  f508
// f510:  f509
// f511:  f510
// f512:  f511
// f513:  f512
// f514:  f513
// f515:  f514
// f516:  f515
// f517:  f516
// f518:  f517
// f519:  f518
// f520:  f519
// f521:  f520
// f522:  f521
// f523:  f522
// f524:  f523
// f525:  f524
// f526:  f525
// f527:  f526
// f528:  f527
// f529:  f528
// f530:  f529
// f531:  f530
// f532:  f531
// f533:  f532
// f534:  f533
// f535:  f534
// f536:  f535
// f537:  f536
// f538:  f537
// f539:  f538
// f540:  f539
// f541:  f540
// f542:  f541
// f543:  f542
// f544:  f543
// f545:  f544
// f546:  f545
// f547:  f546
// f548:  f547
// f549:  f548
// f550:  f549
// f551:  f550
// f552:  f551
// f553:  f552
// f554:  f553
// f555:  f554
// f556:  f555
// f557:  f556
// f558:  f557
// f559:  f558
// f560:  f559
// f561:  f560
// f562:  f561
// f563:  f562
// f564:  f563
// f565:  f564
// f566:  f565
// f567:  f566
// f568:  f567
// f569:  f568
// f570:  f569
// f571:  f570
// f572:  f571
// f573:  f572
// f574:  f573
// f575:  f574
// f576:  f575
// f577:  f576
// f578:  f577
// f579:  f578
// f580:  f579
// f581:  f580
// f582:  f581
// f583:  f582
// f584:  f583
// f585:  f584
// f586:  f585
// f587:  f586
// f588:  f587
// f589:  f588
// f590:  f589
// f591:  f590
// f592:  f591
// f593:  f592
// f594:  f593
// f595:  f594
// f596:  f595
// f597:  f596
// f598:  f597
// f599:  f598
// f600:  f599
// f601:  f600
// f602:  f601
// f603:  f602
// f604:  f603
// f605:  f604
// f606:  f605
// f607:  f606
// f608:  f607
// f609:  f608
// f610:  f609
// f611:  f610
// f612:  f611
// f613:  f612
// f614:  f613
// f615:  f614
// f616:  f615
// f617:  f616
// f618:  f617
// f619:  f618
// f620:  f619
// f621:  f620
// f622:  f621
// f623:  f622
// f624:  f623
// f625:  f624
// f626:  f625
// f627:  f626
// f628:  f627
// f629:  f628
// f630:  f629
// f631:  f630
// f632:  f631
// f633:  f632
// f634:  f633
// f635:  f634
// f636:  f635
// f637:  f636
// f638:  f637
// f639:  f638
// f640:  f639
// f641:  f640
// f642:  f641
// f643:  f642
// f644:  f643
// f645:  f644
// f646:  f645
// f647:  f646
// f648:  f647
// f649:  f648
// f650:  f649
// f651:  f650
// f652:  f651
// f653:  f652
// f654:  f653
// f655:  f654
// f656:  f655
// f657:  f656
// f658:  f657
// f659:  f658
// f660:  f659
// f661:  f660
// f662:  f661
// f663:  f662
// f664:  f663
// f665:  f664
// f666:  f665
// f667:  f666
// f668:  f667
// f669:  f668
// f670:  f669
// f671:  f670
// f672:  f671
// f673:  f672
// f674:  f673
// f675:  f674
// f676:  f675
// f677:  f676
// f678:  f677
// f679:  f678
// f680:  f679
// f681:  f680
// f682:  f681
// f683:  f682
// f684:  f683
// f685:  f684
// f686:  f685
// f687:  f686
// f688:  f687
// f689:  f688
// f690:  f689
// f691:  f690
// f692:  f691
// f693:  f692
// f694:  f693
// f695:  f694
// f696:  f695
// f697:  f696
// f698:  f697
// f699:  f698
// f700:  f699
// f701:  f700
// f702:  f701
// f703:  f702
// f704:  f703
// f705:  f704
// f706:  f705
// f707:  f706
// f708:  f707
// f709:  f708
// f710:  f709
// f711:  f710
// f712:  f711
// f713:  f712
// f714:  f713
// f715:  f714
// f716:  f715
// f717:  f716
// f718:  f717
// f719:  f718
// f720:  f719
// f721:  f720
// f722:  f721
// f723:  f722
// f724:  f723
// f725:  f724
// f726:  f725
// f727:  f726
// f728:  f727
// f729:  f728
// f730:  f729
// f731:  f730
// f732:  f731
// f733:  f732
// f734:  f733
// f735:  f734
// f736:  f735
// f737:  f736
// f738:  f737
// f739:  f738
// f740:  f739
// f741:  f740
// f742:  f741
// f743:  f742
// f744:  f743
// f745:  f744
// f746:  f745
// f747:  f746
// f748:  f747
// f749:  f748
// f750:  f749
// f751:  f750
// f752:  f751
// f753:  f752
// f754:  f753
// f755:  f754
// f756:  f755
// f757:  f756
// f758:  f757
// f759:  f758
// f760:  f759
// f761:  f760
// f762:  f761
// f763:  f762
// f764:  f763
// f765:  f764
// f766:  f765
// f767:  f766
// f768:  f767
// f769:  f768
// f770:  f769
// f771:  f770
// f772:  f771
// f773:  f772
// f774:  f773
// f775:  f774
// f776:  f775
// f777:  f776
// f778:  f777
// f779:  f778
// f780:  f779
// f781:  f780
// f782:  f781
// f783:  f782
// f784:  f783
// f785:  f784
// f786:  f785
// f787:  f786
// f788:  f787
// f789:  f788
// f790:  f789
// f791:  f790
// f792:  f791
// f793:  f792
// f794:  f793
// f795:  f794
// f796:  f795
// f797:  f796
// f798:  f797
// f799:  f798
// f800:  f799
// f801:  f800
// f802:  f801
// f803:  f802
// f804:  f803
// f805:  f804
// f806:  f805
// f807:  f806
// f808:  f807
// f809:  f808
// f810:  f809
// f811:  f810
// f812:  f811
// f813:  f812
// f814:  f813
// f815:  f814
// f816:  f815
// f817:  f816
// f818:  f817
// f819:  f818
// f820:  f819
// f821:  f820
// f822:  f821
// f823:  f822
// f824:  f823
// f825:  f824
// f826:  f825
// f827:  f826
// f828:  f827
// f829:  f828
// f830:  f829
// f831:  f830
// f832:  f831
// f833:  f832
// f834:  f833
// f835:  f834
// f836:  f835
// f837:  f836
// f838:  f837
// f839:  f838
// f840:  f839
// f841:  f840
// f842:  f841
// f843:  f842
// f844:  f843
// f845:  f844
// f846:  f845
// f847:  f846
// f848:  f847
// f849:  f848
// f850:  f849
// f851:  f850
// f852:  f851
// f853:  f852
// f854:  f853
// f855:  f854
// f856:  f855
// f857:  f856
// f858:  f857
// f859:  f858
// f860:  f859
// f861:  f860
// f862:  f861
// f863:  f862
// f864:  f863
// f865:  f864
// f866:  f865
// f867:  f866
// f868:  f867
// f869:  f868
// f870:  f869
// f871:  f870
// f872:  f871
// f873:  f872
// f874:  f873
// f875:  f874
// f876:  f875
// f877:  f876
// f878:  f877
// f879:  f878
// f880:  f879
// f881:  f880
// f882:  f881
// f883:  f882
// f884:  f883
// f885:  f884
// f886:  f885
// f887:  f886
// f888:  f887
// f889:  f888
// f890:  f889
// f891:  f890
// f892:  f891
// f893:  f892
// f894:  f893
// f895:  f894
// f896:  f895
// f897:  f896
// f898:  f897
// f899:  f898
// f900:  f899
// f901:  f900
// f902:  f901
// f903:  f902
// f904:  f903
// f905:  f904
// f906:  f905
// f907:  f906
// f908:  f907
// f909:  f908
// f910:  f909
// f911:  f910
// f912:  f911
// f913:  f912
// f914:  f913
// f915:  f914
// f916:  f915
// f917:  f916
// f918:  f917
// f919:  f918
// f920:  f919
// f921:  f920
// f922:  f921
// f923:  f922
// f924:  f923
// f925:  f924
// f926:  f925
// f927:  f926
// f928:  f927
// f929:  f928
// f930:  f929
// f931:  f930
// f932:  f931
// f933:  f932
// f934:  f933
// f935:  f934
// f936:  f935
// f937:  f936
// f938:  f937
// f939:  f938
// f940:  f939
// f941:  f940
// f942:  f941
// f943:  f942
// f944:  f943
// f945:  f944
// f946:  f945
// f947:  f946
// f948:  f947
// f949:  f948
// f950:  f949
// f951:  f950
// f952:  f951
// f953:  f952
// f954:  f953
// f955:  f954
// f956:  f955
// f957:  f956
// f958:  f957
// f959:  f958
// f960:  f959
// f961:  f960
// f962:  f961
// f963:  f962
// f964:  f963
// f965:  f964
// f966:  f965
// f967:  f966
// f968:  f967
// f969:  f968
// f970:  f969
// f971:  f970
// f972:  f971
// f973:  f972
// f974:  f973
// f975:  f974
// f976:  f975
// f977:  f976
// f978:  f977
// f979:  f978
// f980:  f979
// f981:  f980
// f982:  f981
// f983:  f982
// f984:  f983
// f985:  f984
// f986:  f985
// f987:  f986
// f988:  f987
// f989:  f988
// f990:  f989
// f991:  f990
// f992:  f991
// f993:  f992
// f994:  f993
// f995:  f994
// f996:  f995
// f997:  f996
// f998:  f997
// f999:  f998
// f1000: f999
	`

	if strings.HasSuffix(strings.TrimSpace(in), ".cue --") {
		t.Skip()
	}

	a := txtar.Parse([]byte(in))
	instance := cuetxtar.Load(a, t.TempDir())[0]
	if instance.Err != nil {
		t.Fatal(instance.Err)
	}

	r := runtime.New()

	v, err := r.Build(nil, instance)
	if err != nil {
		t.Fatal(err)
	}

	t.Error(debug.NodeString(r, v, nil))
	// eval.Debug = true
	adt.Verbosity = 1

	e := eval.New(r)
	ctx := e.NewContext(v)
	v.Finalize(ctx)
	adt.Verbosity = 0

	// b := validate.Validate(ctx, v, &validate.Config{Concrete: true})
	// t.Log(errors.Details(b.Err, nil))

	t.Error(debug.NodeString(r, v, nil))

	t.Log(ctx.Stats())
}

func BenchmarkUnifyAPI(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		ctx := cuecontext.New()
		v := ctx.CompileString("")
		for j := 0; j < 500; j++ {
			if j == 400 {
				b.StartTimer()
			}
			v = v.FillPath(cue.ParsePath(fmt.Sprintf("i_%d", i)), i)
		}
	}
}
