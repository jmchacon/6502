	;; These aren't comprehensive WRT flags. Mostly checking the flag state that should correspond to final A or documented side effects.
	C000 48			PHA 
	C001 A9 71		LDA #71
	C003 4B 55		ALR #55
	C005 F0 FE		BEQ * ; Check Z is clear
	C007 90 FE		BCC * ; Check C is set
	C009 30 FE		BMI * ; Check N is clear
	C00B C9 28		CMP #28 ; Verify expected value
	C00D D0 FE		BNE *	; Loop if bad
	C00F 0B 20		ANC #20
	C011 F0 FE		BEQ *	; Check Z is clear
	C013 B0 FE		BCS * ; Make sure C cleared
	C015 30 FE		BMI * ; Check N is clear
	C017 C9 20		CMP #20 ; Make sure the right value
	C019 D0 FE		BNE *   ; Loop if bad
	C01B 38			SEC   ; Reset for other opcode variant
	C01C 2B 40		ANC #40
	C01E D0 FE		BNE * 	; Check Z is set
	C020 B0 FE		BCS *	; Make sure C cleared
	C022 30 FE		BMI *	; Check N is clear
	C024 D8			CLD	; Tests for ARR in non decimal first
	C025 38			SEC	; Set carry so it gets rotated in.
	C026 B8			CLV	; And overflow
	C027 A9 C1		LDA #C1
	C029 6B 55		ARR #55
	C02B F0 FE		BEQ * ; Check Z is clear
	C02D B0 FE		BCS * ; Make sure C cleared
	C02F 10 FE		BPL * ; N should be set
	C031 50 FE		BVC * ; V should be set
	C033 C9 A0		CMP #A0 ; Verify expected value
	C035 18			CLC	; Clear up front since should set this time.
	C036 A9 C1		LDA #C1
	C038 6B C5		ARR #C5
	C03A F0 FE		BEQ * ; Check Z is clear
	C03C 90 FE		BCC * ; Check C got set
	C03E 30 FE		BMI * ; Check N is clear
	C040 70 FE		BVS * ; Check V is clear
	C042 C9 60		CMP #60 ; Verify expected value
	C044 D0 FE		BNE *   ; Loop if bad
	C046 F8			SED	; Decimal version check for ARR
	C047 38			SEC
	C048 B8			CLV
	C049 A9 C5		LDA #C5
	C04B 6B 55		ARR #55
	C04D F0 FE		BEQ * ; Check Z is clear
	C04F B0 FE		BCS * ; Make sure C cleared
	C051 10 FE		BPL * ; N should be set
	C053 50 FE		BVC * ; V should be set
	C055 C9 A8		CMP #A8 ; Should be different in decimal mode
	C057 D0 FE		BNE *   ; Loop if bad
	C059 18			CLC	; Another pass where we check C,!N,!Z,!V
	C05A A9 C5		LDA #c5
	C05C 6B D5		ARR #D5
	C05E F0 FE		BEQ * ; Check Z is clear
	C060 90 FE		BCC * ; Check C got set
	C062 30 FE		BMI * ; Check N is clear
	C064 70 FE		BVS * ; Check V is clear
	C066 C9 C8		CMP #C8 ; Verify expected value (both halves did fixups).
	C068 D0 FE		BNE *   ; Loop if bad
	C06A 8A			TXA
	C06B 48			PHA
	C06C A9 B1		LDA #B1
	C06E A2 F1		LDX #F1
	C070 8B 55		XAA #55
	C072 F0 FE		BEQ * ; Check Z is clear
	C074 90 FE		BCC * ; Check C is still set
	C076 30 FE		BMI * ; Check N is clear
	C078 70 FE		BVS * ; Check V is still clear
	C07A C9 51		CMP #51
	C07C D0 FE		BNE * ; Loop if bad
	C07E 98			TYA
	C07F 48			PHA
	C080 A0 FF		LDY #FF ; Counter for iterations of OAL
	C082 A9 B1		LDA #B1 ;  START
	C084 A2 F1		LDX #F1
	C086 AB 55		OAL #55
	C088 C9 51		CMP #51 ; We ran XAA
	C08A F0 08		BEQ CONT ; Do the DEY
	C08C C9 11		CMP #11	 ; Did OAL (A&#) -> A,X
	C08E D0 FE		BNE *	 ; Loop if neither test matched.
	C090 E0 11		CPX #11
	C092 D0 FE		BNE *	  ; Loop if neither test matched.
	C094 88			DEY	 ; CONT
	C095 D0 EF		BNE START ; Not done
	C097 A9 F0		LDA #F0
	C099 85 FF		STA FF
	C09B A9 12		LDA #12
	C09D 85 00		STA 00 ; Setup (d),y for FF to point at 12F0
	C09F 8D 00 13		STA 1300 ; The final addr + Y. Put 12 there for now.
	C0A2 A9 B5		LDA #B5
	C0A4 A2 D3		LDX #D3
	C0A6 A0 10		LDY #10
	C0A8 93 FF		AHX (FF),Y
	C0AA AD 00 13		LDA 1300
	C0AD C9 10		CMP #10
	C0AF D0 FE		BNE *
	C0B1 A9 FF		LDA #FF
	C0B3 8D 00 13		STA 1300 ; Reset for 2nd call using absolute,Y
	C086 A9 B5		LDA #B5
	C0B8 9F F0 12		AHX 12F0,Y
	C0BB AD 00 13		LDA 1300
	C0BE C9 10		CMP #10
	C0C0 D0 FE		BNE *
	C0C2 BA			TSX ; Need to save S since LAS/TAS will change it, but can't use stack
	C0C3 86 01		STX 01
	C0C5 A9 FF		LDA #FF
	C0C7 8D 00 13		STA 1300 ; Reset for TAS
	C0CA A2 D3		LDX #D3	 ; Reset X as before
	C0CC A9 B5		LDA #B5
	C0CE 9B F0 12		TAS 12F0,Y
	C0D1 AD 00 13		LDA 1300
	C0D4 C9 10		CMP #10
	C0D6 D0 FE		BNE *
	C0D8 BA			TSX
	C0D9 E0 91		CPX #91 ; What S changed to. Can't compare against stashed value, stack might be that?
	C0DB D0 FE		BNE *
	C0DD 8A			TXA
	C0DE 29 10		AND #10
	C0E0 85 02		STA 02 ; precompute expected value from LAS for all regs since it's S&val (from 1300) and we know S
	C0E2 A9 FF		LDA #FF
	C0E4 A2 FF		LDX #FF
	C0E6 BB F0 12		LAS 12F0,Y
	C0E9 C5 02		CMP 02
	C0EB D0 FE		BNE * ; Check A
	C0ED E4 02		CPX 02
	C0EF D0 FE		BNE * ; Check X
	C0F1 BA			TSX
	C0F2 E4 02		CPX 02
	C0F4 D0 FE		BNE * ; Check S
	C0F6 A6 01		LDX 01
	C0F8 9A			TXS ; Restore S
	C0F9 A9 FF		LDA #FF
	C0FB 8D 00 13		STA 1300 ; Reset for SHY
	C0FE A9 B5		LDA #B5
	C100 A2 10		LDX #10
	C102 A0 D3		LDY #D3 ; Use same values as before but swap regs
	C104 9C F0 12		SHY 12F0,X
	C107 AD 00 13		LDA 1300
	C10A C9 10		CMP #10
	C10C D0 FE		BNE *
	C10E A9 FF		LDA #FF
	C110 8D 00 13		STA 1300 ; Reset for SHX
	C113 A9 B5		LDA #B5
	C115 A2 D3		LDX #D3
	C117 A0 10		LDY #10 ; Use same values as before but swap regs
	C119 9E F0 12		SHX 12F0,Y
	C11C AD 00 13		LDA 1300
	C11F C9 10		CMP #10
	C121 D0 FE		BNE *
	C123 F0 FE		BEQ * ; We're done
