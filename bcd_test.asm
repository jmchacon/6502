ERROR equ 0x00
N1    equ 0x01
N2    equ 0x02
N2L   equ 0x03
N2H   equ 0x04
N2H+1 equ 0x05
N1L   equ 0x06
N1H   equ 0x07
DA    equ 0x08
DNVZC equ 0x09
HA    equ 0x0A
HNVZC equ 0x0B
AR    equ 0x0C
CF    equ 0x0D
VF    equ 0x0E
NF    equ 0x0F
ZF    equ 0x10
	
C000 A0 01	TEST    LDY #1    		; initialize Y (used to loop through carry flag values)
C002 84 00	        STY ERROR 	; store 1 in ERROR until the test passes
C004 A9 00	        LDA #0    	; initialize N1 and N2
C006 85 01	        STA N1
C008 85 02	        STA N2
C00A A5 02	LOOP1   LDA N2    	; N2L = N2 & $0F
C00C 29 0F	        AND #$0F  	; [1] see text
C00E 85 03	        STA N2L
C010 A5 02	        LDA N2    	; N2H = N2 & $F0
C012 29 F0	        AND #$F0  	; [2] see text
C014 85 04	        STA N2H
C016 09 0F	        ORA #$0F  	; N2H+1 = (N2 & $F0) + $0F
C018 85 05	        STA N2H+1	
C01A A5 01	LOOP2   LDA N1    	; N1L = N1 & $0F
C01C 29 0F	        AND #$0F  	; [3] see text
C01E 85 06	        STA N1L
C020 A5 01	        LDA N1    	; N1H = N1 & $F0
C022 29 F0	        AND #$F0  	; [4] see text
C024 85 07	        STA N1H
C026 20 4D C0	        JSR ADD
C029 20 0B C1(*)        JSR A6502
C02C 20 E6 C0(*)        JSR COMPARE
C02F D0 1A(*)	        BNE DONE
C031 20 91 C0(*)        JSR SUB
C034 20 14 C1(*)        JSR S6502
C037 20 E6 C0(*)        JSR COMPARE
C03A D0 0F(*)	        BNE DONE
C03C E6 01	        INC N1    	; [5] see text
C03E D0 DA(*)	        BNE LOOP2 	; loop through all 256 values of N1
C040 E6 02	        INC N2    	; [6] see text
C042 D0 C6(*)	        BNE LOOP1 	; loop through all 256 values of N2
C044 88		        DEY
C045 10 C3(*)	        BPL LOOP1 	; loop through both values of the carry flag
C047 A9 00	        LDA #0    	; test passed, so store 0 in ERROR
C049 85 00	        STA ERROR
C04B F0 FE	DONE    BEQ DONE

	;;  Calculate the actual decimal mode accumulator and flags, the accumulator
	;;  and flag results when N1 is added to N2 using binary arithmetic, the
	;;  predicted accumulator result, the predicted carry flag, and the predicted
	;;  V flag
	;;
C04D F8		ADD     SED       	; decimal mode
C04E C0 01	        CPY #1    	; set carry if Y = 1, clear carry if Y = 0
C050 A5 01	        LDA N1
C052 65 02	        ADC N2
C054 85 08	        STA DA    	; actual accumulator result in decimal mode
C056 08		        PHP
C057 68		        PLA
C058 85 09	        STA DNVZC 	; actual flags result in decimal mode
C05A D8		        CLD       	; binary mode
C05B C0 01	        CPY #1    	; set carry if Y = 1, clear carry if Y = 0
C05D A5 01	        LDA N1
C05F 65 02	        ADC N2
C061 85 0A	        STA HA    	; accumulator result of N1+N2 using binary arithmetic

C063 08		        PHP
C064 68		        PLA
C065 85 0B	        STA HNVZC 	; flags result of N1+N2 using binary arithmetic
C067 C0 01	        CPY #1
C069 A5 06	        LDA N1L
C06B 65 03	        ADC N2L
C06D C9 0A	        CMP #$0A
C06F A2 00	        LDX #0
C071 90 06(*)	        BCC A1
C073 E8		        INX
C074 69 05	        ADC #5    	; add 6 (carry is set)
C076 29 0F	        AND #$0F
C078 38		        SEC
C079 05 07	A1      ORA N1H
	;;
	;;  if N1L + N2L <  $0A, then add N2 & $F0
	;;  if N1L + N2L >= $0A, then add (N2 & $F0) + $0F + 1 (carry is set)
	;;
C07B 75 04	        ADC N2H,X
C07D 08		        PHP
C07E B0 04(*)	        BCS A2
C080 C9 A0	        CMP #$A0
C082 90 03(*)	        BCC A3
C084 69 5F	A2      ADC #$5F  	; add $60 (carry is set)
C086 38		        SEC
C087 85 0C	A3      STA AR    	; predicted accumulator result
C089 08		        PHP
C08A 68		        PLA
C08B 85 0D	        STA CF    	; predicted carry result
C08D 68		        PLA
	;;
	;;  note that all 8 bits of the P register are stored in VF
	;;
C08E 85 0E	        STA VF    	; predicted V flags
C090 60		        RTS

	;;  Calculate the actual decimal mode accumulator and flags, and the
	;;  accumulator and flag results when N2 is subtracted from N1 using binary
	;;  arithmetic
	;;
C091 F8		SUB     SED       	; decimal mode
C092 C0 01	        CPY #1    	; set carry if Y = 1, clear carry if Y = 0
C094 A5 01	        LDA N1
C096 E5 02	        SBC N2
C098 85 08	        STA DA    	; actual accumulator result in decimal mode
C09A 08		        PHP
C09B 68		        PLA
C09C 85 09	        STA DNVZC 	; actual flags result in decimal mode
C09E D8		        CLD       	; binary mode
C09F C0 01	        CPY #1    	; set carry if Y = 1, clear carry if Y = 0
C0A1 A5 01	        LDA N1
C0A3 E5 02	        SBC N2
C0A5 85 0A	        STA HA    	; accumulator result of N1-N2 using binary arithmetic

C0A7 08		        PHP
C0A8 68		        PLA
C0A9 85 0B	        STA HNVZC 	; flags result of N1-N2 using binary arithmetic
C0AB 60		        RTS

	;;  Calculate the predicted SBC accumulator result for the 6502 and 65816

	;;
C0AC C0 01	SUB1    CPY #1    	; set carry if Y = 1, clear carry if Y = 0
C0AE A5 06	        LDA N1L
C0B0 E5 03	        SBC N2L
C0B2 A2 00	        LDX #0
C0B4 B0 06(*)	        BCS S11
C0B6 E8		        INX
C0B7 E9 05	        SBC #5    	; subtract 6 (carry is clear)
C0B9 29 0F	        AND #$0F
C0BB 18		        CLC
C0BC 05 07	S11     ORA N1H
	;;
	;;  if N1L - N2L >= 0, then subtract N2 & $F0
	;;  if N1L - N2L <  0, then subtract (N2 & $F0) + $0F + 1 (carry is clear)
	;;
C0BE F5 04	        SBC N2H,X
C0C0 B0 02(*)	        BCS S12
C0C2 E9 5F	        SBC #$5F  	; subtract $60 (carry is clear)
C0C4 85 0C	S12     STA AR
C0C6 60		        RTS

	;;  Calculate the predicted SBC accumulator result for the 6502 and 65C02

	;;
C0C7 C0 01	SUB2    CPY #1    	; set carry if Y = 1, clear carry if Y = 0
C0C9 A5 06	        LDA N1L
C0CB E5 03	        SBC N2L
C0CD A2 00	        LDX #0
C0CF B0 04(*)	        BCS S21
C0D1 E8		        INX
C0D2 29 0F	        AND #$0F
C0D4 18		        CLC
C0D5 05 07	S21     ORA N1H
	;;
	;;  if N1L - N2L >= 0, then subtract N2 & $F0
	;;  if N1L - N2L <  0, then subtract (N2 & $F0) + $0F + 1 (carry is clear)
	;;
C0D7 F5 04	        SBC N2H,X
C0D9 B0 02(*)	        BCS S22
C0DB E9 5F	        SBC #$5F   	; subtract $60 (carry is clear)
C0DD E0 00	S22     CPX #0
C0DF F0 02(*)	        BEQ S23
C0E1 E9 06	        SBC #6
C0E3 85 0C	S23     STA AR     	; predicted accumulator result
C0E5 60		        RTS

	;;  Compare accumulator actual results to predicted results
	;;
	;;  Return:
	;;    Z flag = 1 (BEQ branch) if same
	;;    Z flag = 0 (BNE branch) if different
	;;
C0E6 A5 08	COMPARE LDA DA
C0E8 C5 0C	        CMP AR
C0EA D0 1E(*)	        BNE C1
C0EC A5 09	        LDA DNVZC 	; [7] see text
C0EE 45 0F	        EOR NF
C0F0 29 80	        AND #$80  	; mask off N flag
C0F2 D0 16(*)	        BNE C1
C0F4 A5 09	        LDA DNVZC 	; [8] see text
C0F6 45 0E	        EOR VF
C0F8 29 40	        AND #$40  	; mask off V flag
C0FA D0 0E(*)	        BNE C1    	; [9] see text
C0FC A5 09	        LDA DNVZC
C0FE 45 10	        EOR ZF    	; mask off Z flag
C100 29 02	        AND #2
C102 D0 06(*)	        BNE C1    	; [10] see text
C104 A5 09	        LDA DNVZC
C106 45 0D	        EOR CF
C108 29 01	        AND #1    	; mask off C flag
C10A 60		C1      RTS

	;;  These routines store the predicted values for ADC and SBC for the 6502,
	;;  65C02, and 65816 in AR, CF, NF, VF, and ZF

C10B A5 0E	A6502   LDA VF
	;;
	;;  since all 8 bits of the P register were stored in VF, bit 7 of VF contains
	;;  the N flag for NF
	;;
C10D 85 0F	        STA NF
C10F A5 0B	        LDA HNVZC
C111 85 10	        STA ZF
C113 60		        RTS

C114 20 AC C0(*)S6502   JSR SUB1
C117 A5 0B	        LDA HNVZC
C119 85 0F	        STA NF
C11B 85 0E	        STA VF
C11D 85 10	        STA ZF
C11F 85 0D	        STA CF
C121 60		        RTS

	A65C02  LDA AR
	        PHP
	        PLA
	        STA NF
	        STA ZF
	        RTS

	S65C02  JSR SUB2
	        LDA AR
	        PHP
	        PLA
	        STA NF
	        STA ZF
	        LDA HNVZC
	        STA VF
	        STA CF
	        RTS

	A65816  LDA AR
	        PHP
	        PLA
	        STA NF
	        STA ZF
	        RTS

	S65816  JSR SUB1
	        LDA AR
	        PHP
	        PLA
	        STA NF
	        STA ZF
	        LDA HNVZC
	        STA VF
	        STA CF
		RTS
