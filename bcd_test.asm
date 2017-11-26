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
	
C000 A0 01      TEST    LDY #1    		; initialize Y (used to loop through carry flag values)
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
C029 20 x2l x2h	        JSR A6502
C02C 20 x3l x3h	        JSR COMPARE
C02F D0 x4	        BNE DONE
C031 20 x5l x5h	        JSR SUB
C034 20 x6l x6h	        JSR S6502
C037 20 x3l x3h	        JSR COMPARE
C03A D0 x4	        BNE DONE
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
	        BCC A1
	        INX
	        ADC #5    	; add 6 (carry is set)
	        AND #$0F
	        SEC
	A1      ORA N1H
	;;
	;;  if N1L + N2L <  $0A, then add N2 & $F0
	;;  if N1L + N2L >= $0A, then add (N2 & $F0) + $0F + 1 (carry is set)
	;;
	        ADC N2H,X
	        PHP
	        BCS A2
	        CMP #$A0
	        BCC A3
	A2      ADC #$5F  	; add $60 (carry is set)
	        SEC
	A3      STA AR    	; predicted accumulator result
	        PHP
	        PLA
	        STA CF    	; predicted carry result
	        PLA
	;;
	;;  note that all 8 bits of the P register are stored in VF
	;;
	        STA VF    	; predicted V flags
	        RTS

	;;  Calculate the actual decimal mode accumulator and flags, and the
	;;  accumulator and flag results when N2 is subtracted from N1 using binary
	;;  arithmetic
	;;
	SUB     SED       	; decimal mode
	        CPY #1    	; set carry if Y = 1, clear carry if Y = 0
	        LDA N1
	        SBC N2
	        STA DA    	; actual accumulator result in decimal mode
	        PHP
	        PLA
	        STA DNVZC 	; actual flags result in decimal mode
	        CLD       	; binary mode
	        CPY #1    	; set carry if Y = 1, clear carry if Y = 0
	        LDA N1
	        SBC N2
	        STA HA    	; accumulator result of N1-N2 using binary arithmetic

	        PHP
	        PLA
	        STA HNVZC 	; flags result of N1-N2 using binary arithmetic
	        RTS

	;;  Calculate the predicted SBC accumulator result for the 6502 and 65816

	;;
	SUB1    CPY #1    	; set carry if Y = 1, clear carry if Y = 0
	        LDA N1L
	        SBC N2L
	        LDX #0
	        BCS S11
	        INX
	        SBC #5    	; subtract 6 (carry is clear)
	        AND #$0F
	        CLC
	S11     ORA N1H
	;;
	;;  if N1L - N2L >= 0, then subtract N2 & $F0
	;;  if N1L - N2L <  0, then subtract (N2 & $F0) + $0F + 1 (carry is clear)
	;;
	        SBC N2H,X
	        BCS S12
	        SBC #$5F  	; subtract $60 (carry is clear)
	S12     STA AR
	        RTS

	;;  Calculate the predicted SBC accumulator result for the 6502 and 65C02

	;;
	SUB2    CPY #1    	; set carry if Y = 1, clear carry if Y = 0
	        LDA N1L
	        SBC N2L
	        LDX #0
	        BCS S21
	        INX
	        AND #$0F
	        CLC
	S21     ORA N1H
	;;
	;;  if N1L - N2L >= 0, then subtract N2 & $F0
	;;  if N1L - N2L <  0, then subtract (N2 & $F0) + $0F + 1 (carry is clear)
	;;
	        SBC N2H,X
	        BCS S22
	        SBC #$5F   	; subtract $60 (carry is clear)
	S22     CPX #0
	        BEQ S23
	        SBC #6
	S23     STA AR     	; predicted accumulator result
	        RTS

	;;  Compare accumulator actual results to predicted results
	;;
	;;  Return:
	;;    Z flag = 1 (BEQ branch) if same
	;;    Z flag = 0 (BNE branch) if different
	;;
	COMPARE LDA DA
	        CMP AR
	        BNE C1
	        LDA DNVZC 	; [7] see text
	        EOR NF
	        AND #$80  	; mask off N flag
	        BNE C1
	        LDA DNVZC 	; [8] see text
	        EOR VF
	        AND #$40  	; mask off V flag
	        BNE C1    	; [9] see text
	        LDA DNVZC
	        EOR ZF    	; mask off Z flag
	        AND #2
	        BNE C1    	; [10] see text
	        LDA DNVZC
	        EOR CF
	        AND #1    	; mask off C flag
	C1      RTS

	;;  These routines store the predicted values for ADC and SBC for the 6502,
	;;  65C02, and 65816 in AR, CF, NF, VF, and ZF

	A6502   LDA VF
	;;
	;;  since all 8 bits of the P register were stored in VF, bit 7 of VF contains
	;;  the N flag for NF
	;;
	        STA NF
	        LDA HNVZC
	        STA ZF
	        RTS

	S6502   JSR SUB1
	        LDA HNVZC
	        STA NF
	        STA VF
	        STA ZF
	        STA CF
	        RTS

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
	    
